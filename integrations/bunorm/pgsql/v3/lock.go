package pgsql

import (
    "context"
    "database/sql"
    "hash/fnv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const defaultLockReleaseTimeout = 5 * time.Second

/* NewLocker returns a lockcontract.Locker backed by PostgreSQL session advisory locks (pg_try_advisory_lock / pg_advisory_unlock), the Postgres counterpart of the MySQL GET_LOCK locker. A session advisory lock is held by the connection that took it, so each lock pins a dedicated *sql.Conn for its lifetime and releases on a fresh context, mirroring the MySQL locker's semantics. */
func NewLocker(database *bun.DB, options ...LockerOption) *Locker {
    if nil == database {
        exception.Panic(exception.NewError("pgsql lock database is nil", nil, nil))
    }

    locker := &Locker{
        database:       database,
        releaseTimeout: defaultLockReleaseTimeout,
    }

    for _, option := range options {
        option(locker)
    }

    if 0 >= locker.releaseTimeout {
        locker.releaseTimeout = defaultLockReleaseTimeout
    }

    return locker
}

type LockerOption func(*Locker)

func WithLockReleaseTimeout(releaseTimeout time.Duration) LockerOption {
    return func(locker *Locker) {
        locker.releaseTimeout = releaseTimeout
    }
}

type Locker struct {
    database       *bun.DB
    releaseTimeout time.Duration
}

func (instance *Locker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    keyHigh, keyLow := advisoryLockKey(name)

    return &pgsqlLock{
        database:       instance.database,
        name:           name,
        keyHigh:        keyHigh,
        keyLow:         keyLow,
        releaseTimeout: instance.releaseTimeout,
    }
}

type pgsqlLock struct {
    database       *bun.DB
    name           string
    keyHigh        int32
    keyLow         int32
    releaseTimeout time.Duration

    mutex      sync.Mutex
    connection *sql.Conn
}

func (instance *pgsqlLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.connection {
        if true == instance.heldByConnection(runtimeInstance.Context()) {
            return true, nil
        }

        instance.releaseAndCloseConnection()
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("pgsql lock connection failed", map[string]any{"name": instance.name}, connectionErr)
    }

    var acquired sql.NullBool
    queryErr := connection.QueryRowContext(
        runtimeInstance.Context(),
        "SELECT pg_try_advisory_lock($1, $2)",
        instance.keyHigh,
        instance.keyLow,
    ).Scan(&acquired)
    if nil != queryErr {
        releaseOrphanedLock(connection, instance.keyHigh, instance.keyLow, instance.releaseTimeout)
        connection.Close()

        return false, exception.NewError("pgsql lock acquire failed", map[string]any{"name": instance.name}, queryErr)
    }

    if false == acquired.Valid || false == acquired.Bool {
        connection.Close()

        return false, nil
    }

    instance.connection = connection

    return true, nil
}

func (instance *pgsqlLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return nil
    }

    /* @important release on a fresh context so a canceled request context cannot leave the advisory lock held on the connection returned to the pool, mirroring releaseAndCloseConnection/releaseOrphanedLock */
    releaseCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    _, execErr := instance.connection.ExecContext(
        releaseCtx,
        "SELECT pg_advisory_unlock($1, $2)",
        instance.keyHigh,
        instance.keyLow,
    )
    closeErr := instance.connection.Close()
    instance.connection = nil

    if nil != execErr {
        return exception.NewError("pgsql lock release failed", map[string]any{"name": instance.name}, execErr)
    }

    if nil != closeErr {
        return exception.NewError("pgsql lock connection close failed", map[string]any{"name": instance.name}, closeErr)
    }

    return nil
}

func (instance *pgsqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    if false == instance.heldByConnection(runtimeInstance.Context()) {
        /* heldByConnection reports false both when the lock is genuinely gone and when the check itself failed (for example a canceled request context), and in the latter case the advisory lock may still be held server-side. Release on a fresh context before the connection returns to the pool so a still-held lock can never leak onto a pooled connection, mirroring the mysql locker. */
        instance.releaseAndCloseConnection()

        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

/* heldByConnection reports whether the pinned connection's backend still holds this advisory lock. The two-argument pg_try_advisory_lock form stores the keys as classid/objid with objsubid = 2, recovered here with an int cast that undoes the oid reinterpretation. */
func (instance *pgsqlLock) heldByConnection(ctx context.Context) bool {
    var held sql.NullBool

    queryErr := instance.connection.QueryRowContext(
        ctx,
        `SELECT EXISTS (
            SELECT 1 FROM pg_locks
            WHERE locktype = 'advisory'
              AND classid::int = $1
              AND objid::int = $2
              AND objsubid = 2
              AND pid = pg_backend_pid()
              AND granted
        )`,
        instance.keyHigh,
        instance.keyLow,
    ).Scan(&held)
    if nil != queryErr {
        return false
    }

    return true == held.Valid && true == held.Bool
}

func (instance *pgsqlLock) releaseAndCloseConnection() {
    releaseCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    _, _ = instance.connection.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1, $2)", instance.keyHigh, instance.keyLow)
    instance.connection.Close()
    instance.connection = nil
}

/* releaseOrphanedLock is the best-effort release for the acquire error path: pg_try_advisory_lock may have taken the lock server-side before Scan failed (for example on context cancellation), so release on a fresh context before the connection returns to the pool. */
func releaseOrphanedLock(connection *sql.Conn, keyHigh int32, keyLow int32, releaseTimeout time.Duration) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
    defer cancel()

    _, _ = connection.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1, $2)", keyHigh, keyLow)
}

/* advisoryLockKey hashes the lock name into the two 32-bit halves of a 64-bit advisory key, so arbitrary string names map onto PostgreSQL's integer-keyed advisory locks. */
func advisoryLockKey(name string) (int32, int32) {
    hasher := fnv.New64a()
    _, _ = hasher.Write([]byte(name))
    sum := hasher.Sum64()

    return int32(sum >> 32), int32(sum)
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*pgsqlLock)(nil)
