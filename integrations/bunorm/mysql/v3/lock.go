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

/* mysqlLock carries both spellings of its name: the one the caller gave, which every error context names, and the folded form the server was actually asked for, which the contexts name beside it — a name past the server's limit is folded to a hash-suffixed form, and a diagnostic that showed only the caller's spelling sent the operator to look for a lock the server had never heard of. */
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
        /* the cause is Refresh's to report; here a lock that is no longer held simply falls through to be taken afresh below */
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
        discardConnection(connection)

        return nil
    }

    return connection.Close()
}

/* discardConnection marks the driver connection bad so database/sql closes it instead of returning it to the pool. Ending the MySQL session releases every named lock the connection still holds, which is what makes a still-locked connection safe to abandon rather than reuse. Mirrors the pgsql advisory-lock locker, where the same primitive is written once and called by both doors that need it. */
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

/* verifyPinnedLock asks the pinned session whether it still holds the named lock, and leaves the pin in the state that answer implies. Acquire and Refresh both asked it, spelled twice, with the three outcomes and their two different repairs written out in each; they differ only in what they do with the answer, so only that is left at the call sites.

   The probe runs on a fresh, bounded context so a transient cause — a canceled or expired request context — is never mistaken for a lost lock and does not actively release a still-held one: a MySQL GET_LOCK is held for exactly as long as the pinned session lives, so there is no lease to renew and nothing to lose on a transient error, mirroring the pgsql advisory-lock locker.

   The three outcomes:

     - the probe answers "held": the lock is held, the pin stands;
     - the probe answers NOTHING: that is not an answer of "lost". A server stall past the probe budget, a killed query, a blip on the wire all fail it while the session — and the GET_LOCK the session holds — are untouched. Releasing here handed away a lock this process still held, and the caller reads a failed refresh as "another instance may hold it now" and stops the callback, so the two together put a second holder inside an exclusive section this one had never left. Liveness is the question that decides, and it is the only question the pgsql advisory-lock locker ever asks: a session that still answers still holds its lock, and only a session that is gone has lost it — having already released it server-side, which is why that branch ENDS the connection instead of unlocking on it;
     - the probe answers "not held": the session let the lock go, so there is nothing to release. The connection is healthy, so it goes back to the pool and only the pin is dropped.

   The cause is answered beside the verdict because Refresh names it and Acquire does not. */
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
