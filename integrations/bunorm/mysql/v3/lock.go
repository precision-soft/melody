package mysql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "hash/fnv"
    "strconv"
    "strings"
    "sync"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

const defaultLockReleaseTimeout = 5 * time.Second

/* MySQL rejects user-level lock names longer than 64 characters (ER_USER_LOCK_WRONG_NAME on MySQL 8, so GET_LOCK errors on every attempt), which would make Acquire fail permanently for a long name and never let an exclusive command run. */
const mysqlLockNameMaxLength = 64

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
        lockName:       boundedLockName(name),
        releaseTimeout: instance.releaseTimeout,
    }
}

type mysqlLock struct {
    database       *bun.DB
    name           string
    lockName       string
    releaseTimeout time.Duration

    mutex      sync.Mutex
    connection *sql.Conn
}

func (instance *mysqlLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.connection {
        /* verify on a fresh, bounded context, exactly as Refresh does, so the caller's already-canceled request context is never read as a lost lock */
        verifyCtx, cancelVerify := context.WithTimeout(context.Background(), instance.releaseTimeout)

        var held sql.NullBool
        verifyErr := instance.connection.QueryRowContext(
            verifyCtx,
            "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
            instance.lockName,
        ).Scan(&held)
        cancelVerify()

        if nil == verifyErr && true == held.Valid && true == held.Bool {
            return true, nil
        }

        if nil != verifyErr {
            /* the same distinction Refresh draws: a verify that could not be answered is not a verify that answered "lost". Releasing on it gave the lock away and then reported (false, nil) — the caller was told it never held a lock it was holding a moment earlier, while a competitor walked into the section beside it. */
            if true == instance.pinnedConnectionAlive() {
                return true, nil
            }

            instance.discardPinnedConnection()
        } else {
            /* the verify answered, and its answer is that this session no longer holds the lock: the session let it go, so there is nothing to release — only the pin to drop before taking it afresh below */
            instance.connection.Close()
            instance.connection = nil
        }
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("mysql lock connection failed", map[string]any{"name": instance.name}, connectionErr)
    }

    var acquired sql.NullInt64
    queryErr := connection.QueryRowContext(runtimeInstance.Context(), "SELECT GET_LOCK(?, 0)", instance.lockName).Scan(&acquired)
    if nil != queryErr {
        releaseOrphanedLock(connection, instance.lockName, instance.releaseTimeout)
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

    /* release on a fresh context so a canceled request context cannot leave the GET_LOCK held on the connection returned to the pool, mirroring releaseOrphanedLock */
    releaseCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    _, execErr := instance.connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", instance.lockName)
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

/* best-effort release for the acquire error path: GET_LOCK may have taken the lock server-side before Scan failed (for example on context cancellation), so release on a fresh context before the connection returns to the pool; if RELEASE_LOCK could not be issued the physical session is ended instead so a still-held lock never rides a pooled connection back into reuse */
func releaseOrphanedLock(connection *sql.Conn, name string, releaseTimeout time.Duration) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
    defer cancel()

    _, execErr := connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", name)
    _ = discardOrCloseConnection(connection, execErr)
}

/* returns the connection to the pool with Close when RELEASE_LOCK succeeded; when it could not be issued (releaseErr) the lock may still be held, so the driver connection is marked bad (driver.ErrBadConn) — database/sql then closes the physical session instead of pooling it, which releases the GET_LOCK server-side and guarantees a still-held lock never rides a pooled connection back into reuse. Mirrors the pgsql advisory-lock locker. */
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

    /* probe on a fresh, bounded context so a transient cause (a canceled or expired request context) is never mistaken for a lost lock and does not actively release a still-held lock — a MySQL GET_LOCK is held for as long as the pinned session lives, so there is no lease to renew and nothing to lose on a transient error, mirroring the pgsql advisory-lock locker */
    probeCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    var held sql.NullBool
    queryErr := instance.connection.QueryRowContext(
        probeCtx,
        "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
        instance.lockName,
    ).Scan(&held)
    if nil != queryErr {
        /* a probe that answered NOTHING has not answered "lost": a server stall past the probe budget, a killed query, a blip on the wire all fail it while the session — and the GET_LOCK the session holds — are untouched. Releasing here handed away a lock this process still held, and the caller reads a failed refresh as "another instance may hold it now" and stops the callback, so the two of them together put a second holder inside an exclusive section this one had never left. Liveness is the question that decides, and it is the only question the pgsql advisory-lock locker ever asks: a session that still answers still holds its lock, and only a session that is gone has lost it — having already released it server-side, which is why the dead branch ends the connection instead of unlocking on it. */
        if true == instance.pinnedConnectionAlive() {
            return nil
        }

        instance.discardPinnedConnection()

        return exception.NewError(
            "mysql lock is no longer held",
            map[string]any{"name": instance.name, "lockName": instance.lockName},
            queryErr,
        )
    }

    if false == held.Valid || false == held.Bool {
        instance.connection.Close()
        instance.connection = nil
        return exception.NewError(
            "mysql lock is no longer held",
            map[string]any{"name": instance.name, "lockName": instance.lockName},
            nil,
        )
    }

    return nil
}

/* pinnedConnectionAlive reports whether the pinned session is still up, and therefore whether it still holds the GET_LOCK: MySQL keeps a named lock for exactly as long as the session that took it. It pings on a fresh, bounded context so a canceled or expired request context cannot make a live lock look lost, and it is deliberately not the same query as the refresh probe — the probe asks what the lock's state is, this asks whether there is still a session to hold one. */
func (instance *mysqlLock) pinnedConnectionAlive() bool {
    pingCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    return nil == instance.connection.PingContext(pingCtx)
}

/* discardPinnedConnection ends the pinned session without attempting an unlock and clears the pin. It is for the case where that session is already gone: its named locks were released server-side when it died, so there is nothing left to release, and issuing RELEASE_LOCK on the replacement connection database/sql would hand out would release a lock this process does not hold. Mirrors the pgsql advisory-lock locker. */
func (instance *mysqlLock) discardPinnedConnection() {
    _ = instance.connection.Raw(func(_ any) error {
        return driver.ErrBadConn
    })

    instance.connection = nil
}

/* boundedLockName folds a lock name into a form MySQL's GET_LOCK accepts. Names within mysqlLockNameMaxLength characters are passed through unchanged so existing short names keep their exact server-side identity; a longer name is reduced to a deterministic 64-character form — a rune-safe prefix of the original name for readability, joined to an fnv-64a hash of the full name for uniqueness — mirroring the way the pgsql advisory-lock locker hashes an arbitrary-length name onto its integer key. */
func boundedLockName(name string) string {
    if mysqlLockNameMaxLength >= utf8.RuneCountInString(name) {
        return name
    }

    hasher := fnv.New64a()
    _, _ = hasher.Write([]byte(name))
    suffix := strconv.FormatUint(hasher.Sum64(), 16)

    prefixBudget := mysqlLockNameMaxLength - len(suffix) - 1

    var builder strings.Builder
    prefixLength := 0
    for _, character := range name {
        if prefixLength >= prefixBudget {
            break
        }

        builder.WriteRune(character)
        prefixLength++
    }

    builder.WriteByte('-')
    builder.WriteString(suffix)

    return builder.String()
}

var _ lockcontract.Locker = (*Locker)(nil)
var _ lockcontract.Lock = (*mysqlLock)(nil)
