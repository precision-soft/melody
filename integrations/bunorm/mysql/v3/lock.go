package mysql

import (
    "context"
    "database/sql"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const lockReleaseTimeout = 5 * time.Second

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
        var held sql.NullBool
        verifyErr := instance.connection.QueryRowContext(
            runtimeInstance.Context(),
            "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
            instance.name,
        ).Scan(&held)
        if nil == verifyErr && true == held.Valid && true == held.Bool {
            return true, nil
        }

        instance.releaseAndCloseConnection()
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("mysql lock connection failed", map[string]any{"name": instance.name}, connectionErr)
    }

    var acquired sql.NullInt64
    queryErr := connection.QueryRowContext(runtimeInstance.Context(), "SELECT GET_LOCK(?, 0)", instance.name).Scan(&acquired)
    if nil != queryErr {
        releaseOrphanedLock(connection, instance.name)
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

func (instance *mysqlLock) releaseAndCloseConnection() {
    releaseCtx, cancel := context.WithTimeout(context.Background(), lockReleaseTimeout)
    defer cancel()

    _, _ = instance.connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", instance.name)
    instance.connection.Close()
    instance.connection = nil
}

/** @important best-effort release for the acquire error path: GET_LOCK may have taken the lock server-side before Scan failed (for example on context cancellation), so release on a fresh context before the connection returns to the pool */
func releaseOrphanedLock(connection *sql.Conn, name string) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), lockReleaseTimeout)
    defer cancel()

    _, _ = connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", name)
}

func (instance *mysqlLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return nil
    }

    /** @important release on a fresh context so a canceled request context cannot leave the GET_LOCK held on the connection returned to the pool, mirroring releaseAndCloseConnection/releaseOrphanedLock */
    releaseCtx, cancel := context.WithTimeout(context.Background(), lockReleaseTimeout)
    defer cancel()

    _, execErr := instance.connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", instance.name)
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
        instance.releaseAndCloseConnection()
        return exception.NewError("mysql lock refresh failed", map[string]any{"name": instance.name}, queryErr)
    }

    if false == held.Valid || false == held.Bool {
        instance.connection.Close()
        instance.connection = nil
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*mysqlLock)(nil)
