package mysql

import (
    "database/sql"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

func NewLocker(database *bun.DB) *Locker {
    if nil == database {
        exception.Panic(exception.NewError("mysql lock database is nil", nil, nil))
    }

    return &Locker{
        database: database,
    }
}

type Locker struct {
    database *bun.DB
}

/**
 * CreateLock returns a MySQL advisory lock (GET_LOCK) for the given name.
 *
 * MySQL advisory locks are connection-lifetime: they have NO automatic expiry, so the
 * ttl argument is accepted only for interface compatibility with the lock contract and is
 * intentionally not honored as an auto-expiry. The lock is held until Release is called or
 * the pinned connection is dropped (e.g. the process dies), at which point MySQL releases it.
 * Use the Redis locker when time-based auto-expiry is required.
 */
func (instance *Locker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &mysqlLock{
        database: instance.database,
        name:     name,
    }
}

type mysqlLock struct {
    database *bun.DB
    name     string

    mutex      sync.Mutex
    connection *sql.Conn
}

/**
 * Acquire attempts a non-blocking acquisition (GET_LOCK timeout 0), consistent with the
 * try-acquire semantics of the in-memory and Redis lockers: it returns (false, nil) immediately
 * when the lock is held elsewhere rather than waiting. On success it pins a dedicated connection
 * for the lifetime of the lock so RELEASE_LOCK runs on the same session that holds it.
 */
func (instance *mysqlLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.connection {
        return true, nil
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("mysql lock connection failed", map[string]any{"name": instance.name}, connectionErr)
    }

    var acquired sql.NullInt64
    queryErr := connection.QueryRowContext(runtimeInstance.Context(), "SELECT GET_LOCK(?, 0)", instance.name).Scan(&acquired)
    if nil != queryErr {
        connection.Close()
        return false, exception.NewError("mysql lock acquire failed", map[string]any{"name": instance.name}, queryErr)
    }

    if false == acquired.Valid || 1 != acquired.Int64 {
        connection.Close()
        return false, nil
    }

    instance.connection = connection

    return true, nil
}

func (instance *mysqlLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return nil
    }

    _, execErr := instance.connection.ExecContext(runtimeInstance.Context(), "DO RELEASE_LOCK(?)", instance.name)
    closeErr := instance.connection.Close()
    instance.connection = nil

    if nil != execErr {
        return exception.NewError("mysql lock release failed", map[string]any{"name": instance.name}, execErr)
    }

    if nil != closeErr {
        return exception.NewError("mysql lock connection close failed", map[string]any{"name": instance.name}, closeErr)
    }

    return nil
}

/**
 * Refresh verifies the lock is still held by this connection. Because MySQL advisory locks do
 * not expire, there is no TTL to extend: the ttl argument is ignored and Refresh acts purely as
 * a liveness/ownership check, returning an error if the lock was lost (e.g. the connection dropped).
 */
func (instance *mysqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    var held sql.NullBool
    queryErr := instance.connection.QueryRowContext(
        runtimeInstance.Context(),
        "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
        instance.name,
    ).Scan(&held)
    if nil != queryErr {
        return exception.NewError("mysql lock refresh failed", map[string]any{"name": instance.name}, queryErr)
    }

    if false == held.Valid || false == held.Bool {
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*mysqlLock)(nil)
