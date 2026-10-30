package pgsql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "hash/fnv"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/uptrace/bun"
)

/* LockerConfig is what a locker declares beyond its database; its zero value is the default. */
type LockerConfig struct {
    /* ReleaseTimeout bounds the locker's own round trips, each on a context of its own: the release, the orphan release of a failed acquire and the liveness probe. 5s by default, at least 1ms, unlimited refused, since a release that never ends holds the pinned connection and the lock with it. A release handed a runtime whose context carries an earlier deadline, still ahead, ends at that one. */
    ReleaseTimeout bound.Duration
}

var lockerReleaseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "LockerConfig.ReleaseTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewLocker returns a lockcontract.Locker backed by PostgreSQL session advisory locks (pg_try_advisory_lock / pg_advisory_unlock). A session advisory lock has no expiry: it is held by the backend session that took it until it is explicitly released or that session ends. Each lock therefore pins a dedicated *sql.Conn for its lifetime so the lock stays bound to one backend session, and every release runs on a context without the caller's cancellation so a canceled request context can never strand a held lock on a connection that is returning to the pool. A ReleaseTimeout outside its rule and a nil database panic, the former naming the field. */
func NewLocker(database *bun.DB, config LockerConfig) *Locker {
    return newLocker(database, mustResolveLockerReleaseTimeout(config))
}

func mustResolveLockerReleaseTimeout(config LockerConfig) time.Duration {
    releaseTimeout, releaseTimeoutErr := bound.Resolve(config.ReleaseTimeout, lockerReleaseTimeoutRule)
    if nil != releaseTimeoutErr {
        exception.Panic(exception.FromError(releaseTimeoutErr))
    }

    return releaseTimeout.Value
}

func newLocker(database *bun.DB, releaseTimeout time.Duration) *Locker {
    if nil == database {
        exception.Panic(exception.NewError("pgsql lock database is nil", nil, nil))
    }

    return &Locker{
        database:       database,
        releaseTimeout: releaseTimeout,
    }
}

/* Locker holds the database handle and the release timeout, both fixed at construction. An advisory lock lives in postgres, on the connection that took it, so one Locker serves concurrent callers without a lock of its own. */
type Locker struct {
    database       *bun.DB
    releaseTimeout time.Duration
}

/* CreateLock answers a session lock, which holds until its connection drops and has no lease, so the ttl is ignored and every state of it, unlimited included, is admitted. */
func (instance *Locker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
    keyHigh, keyLow := advisoryLockKey(name)

    return &pgsqlLock{
        database:       instance.database,
        name:           name,
        keyHigh:        keyHigh,
        keyLow:         keyLow,
        releaseTimeout: instance.releaseTimeout,
    }
}

/* pgsqlLock carries the name the caller gave and the two halves of the advisory key the server was asked for, and every error context names both, so a diagnostic can be matched against pg_locks. */
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
        if alive, _ := instance.pinnedConnectionAlive(); true == alive {
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
        orphanCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
        _ = releaseLockedConnection(orphanCtx, connection, instance.keyHigh, instance.keyLow)
        cancel()

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

    releaseCtx, cancel := releaseContextFor(runtimeInstance, instance.releaseTimeout)
    defer cancel()

    if releaseErr := instance.releasePinnedConnection(releaseCtx); nil != releaseErr {
        deadline, _ := releaseCtx.Deadline()

        return exception.NewError(
            "pgsql lock release failed",
            map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow, "releaseTimeout": instance.releaseTimeout.String(), "deadline": deadline.Format(time.RFC3339Nano)},
            releaseErr,
        )
    }

    return nil
}

func (instance *pgsqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, nil)
    }

    /* a session advisory lock has no ttl: it lives as long as its backend session, so Refresh is a liveness probe on a fresh bounded context and only a dead connection fails it; nothing is released on a transient error */
    /* the probe's failure travels as the cause, as the mysql twin's Refresh carries its lost cause */
    if alive, lostCause := instance.pinnedConnectionAlive(); false == alive {
        instance.discardPinnedConnection()

        return exception.NewError("pgsql lock is no longer held", map[string]any{"name": instance.name, "keyHigh": instance.keyHigh, "keyLow": instance.keyLow}, lostCause)
    }

    return nil
}

/* pinnedConnectionAlive reports whether the pinned connection's backend session is still up (and therefore still holds the advisory lock). It pings on a fresh, bounded context so a canceled or expired request context cannot make a live lock look lost. */
/* pinnedConnectionAlive probes the pinned session and answers the probe's failure beside the verdict, so a refusal built on it carries why the lock is read as lost. */
func (instance *pgsqlLock) pinnedConnectionAlive() (bool, error) {
    pingCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    pingErr := instance.connection.PingContext(pingCtx)

    return nil == pingErr, pingErr
}

func (instance *pgsqlLock) releasePinnedConnection(releaseCtx context.Context) error {
    releaseErr := releaseLockedConnection(releaseCtx, instance.connection, instance.keyHigh, instance.keyLow)
    instance.connection = nil

    return releaseErr
}

/* discardPinnedConnection ends the pinned connection's physical session without attempting an unlock (used when the session is already dead), which releases any session advisory lock server-side, and clears the pin. */
func (instance *pgsqlLock) discardPinnedConnection() {
    discardConnection(instance.connection)
    instance.connection = nil
}

/* releaseContextFor answers the context a release runs on: the caller's values without its cancellation, so a cancelled request context still releases instead of stranding the advisory lock on a connection returned to the pool, and the earlier of the locker's budget and the caller's own deadline while that one is still ahead, so a caller's shorter release budget is honoured; a nil or typed-nil runtime releases on the locker's budget. A release cut by either deadline ends the session, which frees the lock server-side. */
func releaseContextFor(runtimeInstance runtimecontract.Runtime, releaseTimeout time.Duration) (context.Context, context.CancelFunc) {
    base := context.Background()
    now := time.Now()
    deadline := now.Add(releaseTimeout)

    if false == isNilInterface(runtimeInstance) {
        if callerContext := runtimeInstance.Context(); false == isNilInterface(callerContext) {
            base = context.WithoutCancel(callerContext)

            /* a deadline already passed is the caller's done context, read as the cancellation is: the release runs on the locker's budget */
            if callerDeadline, hasDeadline := callerContext.Deadline(); true == hasDeadline && true == callerDeadline.After(now) && true == callerDeadline.Before(deadline) {
                deadline = callerDeadline
            }
        }
    }

    return context.WithDeadline(base, deadline)
}

/* releaseLockedConnection releases the advisory lock on the release context and returns the connection to the pool; when pg_advisory_unlock cannot be issued the session is ended instead, which releases every advisory lock server-side. It returns the unlock error, or else the pool-return error. */
func releaseLockedConnection(releaseCtx context.Context, connection *sql.Conn, keyHigh int32, keyLow int32) error {
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
