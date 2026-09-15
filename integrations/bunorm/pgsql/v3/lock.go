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

    if false == instance.pinnedConnectionAlive() {
        instance.discardPinnedConnection()

        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, nil)
    }

    return nil
}

func (instance *pgsqlLock) pinnedConnectionAlive() bool {
    pingCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    return nil == instance.connection.PingContext(pingCtx)
}

func (instance *pgsqlLock) releasePinnedConnection() error {
    releaseErr := releaseLockedConnection(instance.connection, instance.keyHigh, instance.keyLow, instance.releaseTimeout)
    instance.connection = nil

    return releaseErr
}

func (instance *pgsqlLock) discardPinnedConnection() {
    discardConnection(instance.connection)
    instance.connection = nil
}

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

func discardConnection(connection *sql.Conn) {
    _ = connection.Raw(func(_ any) error {
        return driver.ErrBadConn
    })
}

func advisoryLockKey(name string) (int32, int32) {
    hasher := fnv.New64a()
    _, _ = hasher.Write([]byte(name))
    sum := hasher.Sum64()

    return int32(sum >> 32), int32(sum)
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*pgsqlLock)(nil)
