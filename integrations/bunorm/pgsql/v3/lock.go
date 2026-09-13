package pgsql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "hash/fnv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const defaultLockReleaseTimeout = 5 * time.Second

/* NewLocker returns a lockcontract.Locker backed by PostgreSQL session advisory locks (pg_try_advisory_lock / pg_advisory_unlock). A session advisory lock has no expiry: it is held by the backend session that took it until it is explicitly released or that session ends. Each lock therefore pins a dedicated *sql.Conn for its lifetime so the lock stays bound to one backend session, and every release runs on a fresh context so a canceled request context can never strand a held lock on a connection that is returning to the pool. */
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

/* pgsqlLock carries both spellings of its name: the one the caller gave, which every error context names, and the two halves of the advisory key the server was actually asked for, which the contexts name beside it — a diagnostic that showed only the caller's spelling left the operator with nothing to match against pg_locks. */
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
        /* a session advisory lock is held for as long as its backend session lives, so a still-alive pinned connection still holds this lock and re-acquire is a no-op. Liveness is probed on a fresh context so a canceled request context is never read as a lost lock. A dead connection means the session — and the advisory lock with it — is already gone server-side, so discard the pin and take the lock afresh below. */
        if true == instance.pinnedConnectionAlive() {
            return true, nil
        }

        instance.discardPinnedConnection()
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("pgsql lock connection failed", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, connectionErr)
    }

    var acquired sql.NullBool
    queryErr := connection.QueryRowContext(
        runtimeInstance.Context(),
        "SELECT pg_try_advisory_lock($1, $2)",
        instance.keyHigh,
        instance.keyLow,
    ).Scan(&acquired)
    if nil != queryErr {
        /* pg_try_advisory_lock may have taken the lock server-side before Scan failed (for example on context cancellation), so release it — ending the session if the unlock cannot be issued — before the connection returns to the pool. */
        _ = releaseLockedConnection(connection, instance.keyHigh, instance.keyLow, instance.releaseTimeout)

        return false, exception.NewError("pgsql lock acquire failed", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, queryErr)
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

    if releaseErr := instance.releasePinnedConnection(); nil != releaseErr {
        return exception.NewError("pgsql lock release failed", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, releaseErr)
    }

    return nil
}

func (instance *pgsqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, nil)
    }

    /* a PostgreSQL session advisory lock has no ttl to extend: it is held for as long as its backend session lives. "Refresh" is therefore a liveness probe — if the pinned connection still answers, the lock is still held and there is nothing to renew. The probe runs on a fresh, bounded context so a transient cause (a canceled or expired request context) is never mistaken for a lost lock — unlike a ttl-based lease there is nothing here to lose on a transient error, so we must not release on it. Only a genuinely dead connection (its session, and so the lock, already gone) fails the refresh. */
    if false == instance.pinnedConnectionAlive() {
        instance.discardPinnedConnection()

        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, nil)
    }

    return nil
}

/* pinnedConnectionAlive reports whether the pinned connection's backend session is still up (and therefore still holds the advisory lock). It pings on a fresh, bounded context so a canceled or expired request context cannot make a live lock look lost. */
func (instance *pgsqlLock) pinnedConnectionAlive() bool {
    pingCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    return nil == instance.connection.PingContext(pingCtx)
}

/* releasePinnedConnection releases the lock held by the pinned connection and clears the pin. */
func (instance *pgsqlLock) releasePinnedConnection() error {
    releaseErr := releaseLockedConnection(instance.connection, instance.keyHigh, instance.keyLow, instance.releaseTimeout)
    instance.connection = nil

    return releaseErr
}

/* discardPinnedConnection ends the pinned connection's physical session without attempting an unlock (used when the session is already dead), which releases any session advisory lock server-side, and clears the pin. */
func (instance *pgsqlLock) discardPinnedConnection() {
    discardConnection(instance.connection)
    instance.connection = nil
}

/* releaseLockedConnection releases the advisory lock held by connection and returns the connection to the pool. If pg_advisory_unlock cannot be issued (a failed or timed-out unlock), the lock may still be held, so the physical session is ended instead — which releases every session advisory lock server-side — guaranteeing a still-held lock never rides a pooled connection back into reuse. The unlock runs on a fresh context so a canceled request context cannot strand the lock. Returns the unlock error, or otherwise the pool-return (Close) error, if any. */
func releaseLockedConnection(connection *sql.Conn, keyHigh int32, keyLow int32, releaseTimeout time.Duration) error {
    releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
    defer cancel()

    _, unlockErr := connection.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1, $2)", keyHigh, keyLow)
    if nil != unlockErr {
        discardConnection(connection)

        return unlockErr
    }

    return connection.Close()
}

/* discardConnection marks the driver connection bad so database/sql closes it instead of returning it to the pool. Ending the PostgreSQL session releases any session advisory lock the connection still holds, which is what makes a still-locked connection safe to abandon rather than reuse. */
func discardConnection(connection *sql.Conn) {
    _ = connection.Raw(func(_ any) error {
        return driver.ErrBadConn
    })
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
