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

/* MySQL refuses a user-level lock name longer than 64 characters, so a longer name is folded by boundedLockName */
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

/* mysqlLock carries the name the caller gave and the folded form the server was asked for, and every error context names both. */
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
        /* the cause is Refresh's to report; here a lock that is not held falls through to be taken afresh below */
        if stillHeld, _ := instance.verifyPinnedLock(); true == stillHeld {
            return true, nil
        }
    }

    connection, connectionErr := instance.database.DB.Conn(runtimeInstance.Context())
    if nil != connectionErr {
        return false, exception.NewError("mysql lock connection failed", map[string]any{"name": instance.name, "lockName": instance.lockName}, connectionErr)
    }

    var acquired sql.NullInt64
    queryErr := connection.QueryRowContext(runtimeInstance.Context(), "SELECT GET_LOCK(?, 0)", instance.lockName).Scan(&acquired)
    if nil != queryErr {
        releaseOrphanedLock(connection, instance.lockName, instance.releaseTimeout)
        connection.Close()
        return false, exception.NewError("mysql lock acquire failed", map[string]any{"name": instance.name, "lockName": instance.lockName}, queryErr)
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
        return exception.NewError("mysql lock release failed", map[string]any{"name": instance.name, "lockName": instance.lockName}, execErr)
    }

    if nil != closeErr {
        return exception.NewError("mysql lock connection close failed", map[string]any{"name": instance.name, "lockName": instance.lockName}, closeErr)
    }

    return nil
}

/* releaseOrphanedLock releases on a fresh context on the acquire error path, since GET_LOCK may have taken the lock server-side before Scan failed; when RELEASE_LOCK cannot be issued the session is ended instead, so a held lock never returns to the pool. */
func releaseOrphanedLock(connection *sql.Conn, name string, releaseTimeout time.Duration) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
    defer cancel()

    _, execErr := connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", name)
    _ = discardOrCloseConnection(connection, execErr)
}

/* discardOrCloseConnection returns the connection to the pool when RELEASE_LOCK succeeded, and otherwise marks it bad so database/sql ends the session, which releases the lock server-side. */
func discardOrCloseConnection(connection *sql.Conn, releaseErr error) error {
    if nil != releaseErr {
        discardConnection(connection)

        return nil
    }

    return connection.Close()
}

/* discardConnection marks the driver connection bad so database/sql closes it instead of pooling it; ending the session releases every named lock it holds. */
func discardConnection(connection *sql.Conn) {
    _ = connection.Raw(func(_ any) error {
        return driver.ErrBadConn
    })
}

func (instance *mysqlLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.connection {
        return exception.NewError("mysql lock is no longer held", map[string]any{"name": instance.name, "lockName": instance.lockName}, nil)
    }

    stillHeld, lostCause := instance.verifyPinnedLock()
    if false == stillHeld {
        return exception.NewError(
            "mysql lock is no longer held",
            map[string]any{"name": instance.name, "lockName": instance.lockName},
            lostCause,
        )
    }

    return nil
}

/* verifyPinnedLock asks the pinned session whether it still holds the named lock and leaves the pin in the state the answer implies, on a fresh bounded context so a cancelled request context is never read as a lost lock. "Held" keeps the pin; no answer is decided by the session's liveness, since a GET_LOCK lives exactly as long as its session and releasing on a stall would put a second holder in the exclusive section; "not held" returns the healthy connection to the pool and drops the pin. The cause is answered beside the verdict for Refresh to name. */
func (instance *mysqlLock) verifyPinnedLock() (bool, error) {
    probeCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    var held sql.NullBool
    probeErr := instance.connection.QueryRowContext(
        probeCtx,
        "SELECT IS_USED_LOCK(?) = CONNECTION_ID()",
        instance.lockName,
    ).Scan(&held)

    if nil != probeErr {
        if true == instance.pinnedConnectionAlive() {
            return true, nil
        }

        instance.discardPinnedConnection()

        return false, probeErr
    }

    if true == held.Valid && true == held.Bool {
        return true, nil
    }

    instance.connection.Close()
    instance.connection = nil

    return false, nil
}

/* pinnedConnectionAlive reports whether the pinned session is still up, and therefore whether it still holds the GET_LOCK: MySQL keeps a named lock for exactly as long as the session that took it. It pings on a fresh, bounded context so a canceled or expired request context cannot make a live lock look lost, and it is deliberately not the same query as the refresh probe — the probe asks what the lock's state is, this asks whether there is still a session to hold one. */
func (instance *mysqlLock) pinnedConnectionAlive() bool {
    pingCtx, cancel := context.WithTimeout(context.Background(), instance.releaseTimeout)
    defer cancel()

    return nil == instance.connection.PingContext(pingCtx)
}

/* discardPinnedConnection ends the pinned session without attempting an unlock and clears the pin. It is for the case where that session is already gone: its named locks were released server-side when it died, so there is nothing left to release, and issuing RELEASE_LOCK on the replacement connection database/sql would hand out would release a lock this process does not hold. Mirrors the pgsql advisory-lock locker. */
func (instance *mysqlLock) discardPinnedConnection() {
    discardConnection(instance.connection)

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
