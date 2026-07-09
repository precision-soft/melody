package mysql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const defaultLockReleaseTimeout = 5 * time.Second

func NewLocker(database *bun.DB, options ...LockerOption) *Locker {
    if nil == database {
        exception.Panic(exception.NewError("mysql lock database is nil", nil, nil))
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
    return &mysqlLock{
        database:       instance.database,
        name:           name,
        releaseTimeout: instance.releaseTimeout,
    }
}

type mysqlLock struct {
    database       *bun.DB
    name           string
    releaseTimeout time.Duration

    mutex      sync.Mutex
    connection *sql.Conn
}

func (instance *mysqlLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.connection {
        /* @important probe on a fresh, bounded context, exactly as Refresh does: the caller's request context may already be canceled, and a verify that fails for that reason would be mistaken for a lost lock and drive releaseAndCloseConnection() — actively releasing a lock this process still holds */
        verifyCtx, cancelVerify := context.WithTimeout(context.Background(), instance.releaseTimeout)

        var held sql.NullBool
        verifyErr := instance.connection.QueryRowContext(
            verifyCtx,
            "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
            instance.name,
        ).Scan(&held)
        cancelVerify()

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
        releaseOrphanedLock(connection, instance.name, instance.releaseTimeout)
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

    /* @important release on a fresh context so a canceled request context cannot leave the GET_LOCK held on the connection returned to the pool, mirroring releaseAndCloseConnection/releaseOrphanedLock */
    releaseCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    _, execErr := instance.connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", instance.name)
    closeErr := discardOrCloseConnection(instance.connection, execErr)
    instance.connection = nil

    if nil != execErr {
        return exception.NewError("mysql lock release failed", map[string]any{"name": instance.name}, execErr)
    }

    if nil != closeErr {
        return exception.NewError("mysql lock connection close failed", map[string]any{"name": instance.name}, closeErr)
    }

    return nil
}

/* @important best-effort release for the acquire error path: GET_LOCK may have taken the lock server-side before Scan failed (for example on context cancellation), so release on a fresh context before the connection returns to the pool; if RELEASE_LOCK could not be issued the physical session is ended instead so a still-held lock never rides a pooled connection back into reuse */
func releaseOrphanedLock(connection *sql.Conn, name string, releaseTimeout time.Duration) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
    defer cancel()

    _, execErr := connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", name)
    _ = discardOrCloseConnection(connection, execErr)
}

/* @important returns the connection to the pool with Close when RELEASE_LOCK succeeded; when it could not be issued (releaseErr) the lock may still be held, so the driver connection is marked bad (driver.ErrBadConn) — database/sql then closes the physical session instead of pooling it, which releases the GET_LOCK server-side and guarantees a still-held lock never rides a pooled connection back into reuse. Mirrors the pgsql advisory-lock locker. */
func discardOrCloseConnection(connection *sql.Conn, releaseErr error) error {
    if nil != releaseErr {
        _ = connection.Raw(func(_ any) error {
            return driver.ErrBadConn
        })

        return nil
    }

    return connection.Close()
}

func (instance *mysqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    /* @important probe on a fresh, bounded context so a transient cause (a canceled or expired request context) is never mistaken for a lost lock and does not actively release a still-held lock — a MySQL GET_LOCK is held for as long as the pinned session lives, so there is no lease to renew and nothing to lose on a transient error, mirroring the pgsql advisory-lock locker */
    probeCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    var held sql.NullBool
    queryErr := instance.connection.QueryRowContext(
        probeCtx,
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

func (instance *mysqlLock) releaseAndCloseConnection() {
    releaseCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    _, execErr := instance.connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", instance.name)
    _ = discardOrCloseConnection(instance.connection, execErr)
    instance.connection = nil
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*mysqlLock)(nil)
