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
        /** The pinned connection is in an unknown state; drop it so a later Acquire genuinely
            re-acquires on a fresh connection instead of taking the "already held" fast path. */
        instance.connection.Close()
        instance.connection = nil
        return exception.NewError("mysql lock refresh failed", map[string]any{"name": instance.name}, queryErr)
    }

    if false == held.Valid || false == held.Bool {
        /** The lock was lost (session killed or forcibly released). Drop the connection so the lock
            object reflects the lost state; otherwise a later Acquire sees a non-nil connection and
            falsely reports the lock as still held without re-issuing GET_LOCK. */
        instance.connection.Close()
        instance.connection = nil
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*mysqlLock)(nil)
