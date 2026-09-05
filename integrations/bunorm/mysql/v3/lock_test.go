package mysql

import (
    "context"
    "database/sql"
    "errors"
    "os"
    "strconv"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    _ "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

func newLockRuntime() runtimecontract.Runtime {
    return newLockRuntimeWithContext(context.Background())
}

func newLockRuntimeWithContext(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

func TestMysqlLock_MutualExclusionAndRelease(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    runtimeInstance := newLockRuntime()

    name := "melody_lock_test"

    first := locker.CreateLock(name, 0)
    second := locker.CreateLock(name, 0)

    acquired, acquireErr := first.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected first acquire to succeed: %v %v", acquired, acquireErr)
    }

    contended, contendedErr := second.Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected contention while held: %v %v", contended, contendedErr)
    }

    if releaseErr := first.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    afterRelease, afterReleaseErr := second.Acquire(runtimeInstance)
    if nil != afterReleaseErr || false == afterRelease {
        t.Fatalf("expected acquire after release: %v %v", afterRelease, afterReleaseErr)
    }

    if releaseErr := second.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("second release: %v", releaseErr)
    }
}

func TestMysqlLock_RefreshReportsLostLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_lock_refresh_test", 0)

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to fail before the lock is acquired")
    }

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil != refreshErr {
        t.Fatalf("expected refresh to succeed while held: %v", refreshErr)
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to fail after release")
    }
}

func TestMysqlLock_ReacquiresAfterRefreshDetectsLostLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    runtimeInstance := newLockRuntime()

    name := "melody_lock_reacquire"

    lock := locker.CreateLock(name, 0)
    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    var ownerId sql.NullInt64
    if ownerErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_USED_LOCK(?)", name).Scan(&ownerId); nil != ownerErr {
        t.Fatalf("read lock owner: %v", ownerErr)
    }
    if false == ownerId.Valid {
        t.Fatalf("expected the lock to be held by a session")
    }
    if _, killErr := sqldb.ExecContext(runtimeInstance.Context(), "KILL "+strconv.FormatInt(ownerId.Int64, 10)); nil != killErr {
        t.Logf("kill returned (tolerated): %v", killErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to detect the lost lock")
    }

    reacquired, reacquireErr := lock.Acquire(runtimeInstance)
    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected re-acquire to succeed: %v %v", reacquired, reacquireErr)
    }

    var heldBy sql.NullInt64
    if heldErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_USED_LOCK(?)", name).Scan(&heldBy); nil != heldErr {
        t.Fatalf("read lock holder after re-acquire: %v", heldErr)
    }
    if false == heldBy.Valid {
        t.Fatalf("expected the lock to be genuinely held after re-acquire")
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }
}

func TestMysqlLock_AcquireAfterAKilledSessionTakesTheLockAfresh(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    name := "melody_lock_verify_error_release"

    lock := locker.CreateLock(name, 0)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    /* induce a GENUINE verify error by killing the pinned session; a canceled request context must NOT do this (see TestMysqlLock_AcquireCanceledRuntimeContextKeepsHeldLock) */
    var ownerId sql.NullInt64
    if ownerErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&ownerId); nil != ownerErr {
        t.Fatalf("read lock owner: %v", ownerErr)
    }
    if false == ownerId.Valid {
        t.Fatalf("expected the lock to be held by a session")
    }
    if _, killErr := sqldb.ExecContext(context.Background(), "KILL "+strconv.FormatInt(ownerId.Int64, 10)); nil != killErr {
        t.Logf("kill returned (tolerated): %v", killErr)
    }

    /* the verify now fails for real: the lock object must drop the dead connection and take the lock afresh. KILL is asynchronous — the server flags the thread and only releases its locks once that thread notices — so a single immediate attempt races the cleanup and reads GET_LOCK as 0 (held, no error). Retry until the dead session lets go. */
    var reacquired bool
    var reacquireErr error

    reacquireDeadline := time.Now().Add(10 * time.Second)
    for {
        reacquired, reacquireErr = lock.Acquire(newLockRuntime())
        if nil != reacquireErr || true == reacquired {
            break
        }
        if true == time.Now().After(reacquireDeadline) {
            break
        }

        time.Sleep(20 * time.Millisecond)
    }

    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected re-acquire after a genuine verify error: %v %v", reacquired, reacquireErr)
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    var holder sql.NullInt64
    if holderErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&holder); nil != holderErr {
        t.Fatalf("read lock holder: %v", holderErr)
    }
    if true == holder.Valid {
        t.Fatalf("lock was orphaned: still held by session %d after release", holder.Int64)
    }
}

/* Mirrors TestMysqlLock_RefreshCanceledRuntimeContextKeepsHeldLock: a canceled request context is a transient caller-side condition, not a lost lock. Re-acquiring under one must not be mistaken for a verify error and must not RELEASE_LOCK a lock this process still holds. */
func TestMysqlLock_AcquireCanceledRuntimeContextKeepsHeldLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    name := "melody_lock_acquire_canceled_ctx"

    lock := locker.CreateLock(name, 0)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    reacquired, reacquireErr := lock.Acquire(newLockRuntimeWithContext(cancelledContext))
    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected the re-acquire on a canceled context to report the lock as still held: %v %v", reacquired, reacquireErr)
    }

    var holder sql.NullInt64
    if holderErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&holder); nil != holderErr {
        t.Fatalf("read lock holder: %v", holderErr)
    }
    if false == holder.Valid {
        t.Fatal("a canceled request context released a lock this process still held")
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }
}

func TestMysqlLock_RefreshCanceledRuntimeContextKeepsHeldLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    name := "melody_lock_refresh_canceled_context_keeps_lock"

    lock := locker.CreateLock(name, 0)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if refreshErr := lock.Refresh(newLockRuntimeWithContext(cancelledContext), 0); nil != refreshErr {
        t.Fatalf("expected refresh to succeed despite the canceled runtime context: %v", refreshErr)
    }

    var holder sql.NullInt64
    if holderErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&holder); nil != holderErr {
        t.Fatalf("read lock holder: %v", holderErr)
    }
    if false == holder.Valid {
        t.Fatalf("lock was released: a canceled runtime context must not release a still-held lock")
    }

    if releaseErr := lock.Release(newLockRuntime()); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if holderErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&holder); nil != holderErr {
        t.Fatalf("read lock holder after release: %v", holderErr)
    }
    if true == holder.Valid {
        t.Fatalf("lock still held by session %d after release", holder.Int64)
    }
}

func TestMysqlLock_ReentrantAcquireDetectsLostLockWithoutRefresh(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    runtimeInstance := newLockRuntime()

    name := "melody_lock_reentrant_no_refresh"

    lock := locker.CreateLock(name, 0)
    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    var ownerId sql.NullInt64
    if ownerErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_USED_LOCK(?)", name).Scan(&ownerId); nil != ownerErr {
        t.Fatalf("read lock owner: %v", ownerErr)
    }
    if false == ownerId.Valid {
        t.Fatalf("expected the lock to be held by a session")
    }
    if _, killErr := sqldb.ExecContext(runtimeInstance.Context(), "KILL "+strconv.FormatInt(ownerId.Int64, 10)); nil != killErr {
        t.Logf("kill returned (tolerated): %v", killErr)
    }

    /* KILL only flags the session; the GET_LOCK stays held until that session actually ends. Acquire probes with GET_LOCK(?, 0), which never waits, so the competitor below must not run before the kill has landed. */
    lockFreed := false
    for attempt := 0; attempt < 100; attempt++ {
        var free sql.NullInt64
        if freeErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_FREE_LOCK(?)", name).Scan(&free); nil != freeErr {
            t.Fatalf("read lock availability: %v", freeErr)
        }
        if true == free.Valid && 1 == free.Int64 {
            lockFreed = true

            break
        }

        time.Sleep(10 * time.Millisecond)
    }
    if false == lockFreed {
        t.Fatalf("the killed session never released the lock")
    }

    competitor := locker.CreateLock(name, 0)
    competitorAcquired, competitorErr := competitor.Acquire(runtimeInstance)
    if nil != competitorErr || false == competitorAcquired {
        t.Fatalf("expected the competitor to acquire the freed lock: %v %v", competitorAcquired, competitorErr)
    }
    defer competitor.Release(runtimeInstance)

    stillHeld, reacquireErr := lock.Acquire(runtimeInstance)
    if true == stillHeld {
        lock.Release(runtimeInstance)
        t.Fatalf("reentrant Acquire returned true while the competitor holds the lock; mutual exclusion was violated (err=%v)", reacquireErr)
    }
}

func TestMysqlLock_ReleaseOnCanceledContextStillReleases(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := NewLocker(database)
    name := "melody_lock_release_canceled_context"

    lock := locker.CreateLock(name, 0)

    acquired, acquireErr := lock.Acquire(newLockRuntime())
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if releaseErr := lock.Release(newLockRuntimeWithContext(cancelledContext)); nil != releaseErr {
        t.Fatalf("expected release to succeed even with a canceled context: %v", releaseErr)
    }

    var holder sql.NullInt64
    if holderErr := sqldb.QueryRowContext(context.Background(), "SELECT IS_USED_LOCK(?)", name).Scan(&holder); nil != holderErr {
        t.Fatalf("read lock holder: %v", holderErr)
    }
    if true == holder.Valid {
        t.Fatalf("lock was orphaned: still held by session %d after release on a canceled context", holder.Int64)
    }
}

func newOfflineLockDatabase(t *testing.T) *bun.DB {
    t.Helper()

    sqldb, openErr := sql.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/db")
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    t.Cleanup(func() {
        sqldb.Close()
    })

    return bun.NewDB(sqldb, mysqldialect.New())
}

func TestNewLocker_DefaultReleaseTimeout(t *testing.T) {
    locker := NewLocker(newOfflineLockDatabase(t))

    if defaultLockReleaseTimeout != locker.releaseTimeout {
        t.Fatalf("expected default release timeout %s, got %s", defaultLockReleaseTimeout, locker.releaseTimeout)
    }
}

func TestBoundedLockName_ShortNamePassesThrough(t *testing.T) {
    name := "melody:command:something"

    if bounded := boundedLockName(name); bounded != name {
        t.Fatalf("expected a name within the MySQL limit to pass through unchanged, got %q", bounded)
    }
}

/* A lock name longer than MySQL's 64-character user-level-lock limit would make GET_LOCK error on every Acquire, so RunExclusive fails closed forever and the wrapped job never runs. boundedLockName must fold such a name onto a form MySQL accepts. */
func TestBoundedLockName_LongNameFitsMysqlLimit(t *testing.T) {
    name := "melody:command:" + strings.Repeat("a", 80)

    if mysqlLockNameMaxLength >= utf8.RuneCountInString(name) {
        t.Fatalf("test precondition: the name must exceed the MySQL limit")
    }

    bounded := boundedLockName(name)

    if got := utf8.RuneCountInString(bounded); got > mysqlLockNameMaxLength {
        t.Fatalf("bounded lock name has %d characters, exceeding the MySQL limit of %d", got, mysqlLockNameMaxLength)
    }
    if bounded == name {
        t.Fatalf("expected a name over the limit to be reduced, got the raw name")
    }
}

func TestBoundedLockName_DeterministicAndDistinct(t *testing.T) {
    first := "melody:command:" + strings.Repeat("a", 80)
    second := "melody:command:" + strings.Repeat("b", 80)

    if boundedLockName(first) != boundedLockName(first) {
        t.Fatalf("expected boundedLockName to be deterministic")
    }
    if boundedLockName(first) == boundedLockName(second) {
        t.Fatalf("expected different long names to map to different bounded lock names")
    }
}

func TestCreateLock_LongNameYieldsMysqlCompatibleLockName(t *testing.T) {
    locker := NewLocker(newOfflineLockDatabase(t))

    name := "melody:command:" + strings.Repeat("a", 80)

    lock, isMysqlLock := locker.CreateLock(name, 0).(*mysqlLock)
    if false == isMysqlLock {
        t.Fatalf("expected a *mysqlLock")
    }

    if lock.name != name {
        t.Fatalf("expected the raw name to be retained for diagnostics, got %q", lock.name)
    }
    if got := utf8.RuneCountInString(lock.lockName); got > mysqlLockNameMaxLength {
        t.Fatalf("GET_LOCK would receive a %d-character name, which MySQL rejects", got)
    }
}

func TestNewLocker_ReleaseTimeoutOverridePropagatesToLock(t *testing.T) {
    locker := NewLocker(newOfflineLockDatabase(t), WithLockReleaseTimeout(2*time.Second))

    if 2*time.Second != locker.releaseTimeout {
        t.Fatalf("expected overridden release timeout 2s, got %s", locker.releaseTimeout)
    }

    lock, isMysqlLock := locker.CreateLock("name", 0).(*mysqlLock)
    if false == isMysqlLock {
        t.Fatalf("expected a *mysqlLock")
    }

    if 2*time.Second != lock.releaseTimeout {
        t.Fatalf("expected the lock to inherit the release timeout 2s, got %s", lock.releaseTimeout)
    }
}

/* poisonProbeSelect makes every row-returning statement on the lock's own pinned session answer no
   rows, so the refresh probe's Scan fails while the session — and the GET_LOCK it holds — live on.
   It is the shape of a server stall past the probe budget or a KILL QUERY: the probe cannot answer,
   and the lock is nonetheless still held. */
func poisonProbeSelect(t *testing.T, lock *mysqlLock) {
    t.Helper()

    if _, poisonErr := lock.connection.ExecContext(
        context.Background(), "SET SESSION sql_select_limit = 0",
    ); nil != poisonErr {
        t.Fatalf("poison the pinned session: %v", poisonErr)
    }
}

/* observeLockHolder answers the id of the session MySQL reports as holding the named lock, or zero
   when nobody holds it, read on a pool the test has not poisoned: a probe that reports on the
   resource it broke reports nothing. A read error fails the test rather than being folded into a
   zero. The IDENTITY and not merely the presence is the observable, because a locker that drops the
   lock and takes it again on a fresh session ends in the same place as one that never let go — the
   difference between them is the window in between, and the session id is what names it. */
func observeLockHolder(t *testing.T, database *sql.DB, name string) int64 {
    t.Helper()

    var holder sql.NullInt64
    if scanErr := database.QueryRowContext(
        context.Background(), "SELECT IS_USED_LOCK(?)", name,
    ).Scan(&holder); nil != scanErr {
        t.Fatalf("observe the lock holder: %v", scanErr)
    }

    if false == holder.Valid {
        return 0
    }

    return holder.Int64
}

/* A refresh probe that could not be ANSWERED is not a probe that answered "lost". MySQL holds a
   named lock for exactly as long as the session that took it, so a live session still holds its
   lock however the probe fared; releasing on it handed the lock away while the caller — which reads
   a failed refresh as "another instance may hold it now" — stopped the callback, putting a second
   holder inside an exclusive section this one had never left. */
func TestMysqlLock_RefreshOnAnUnansweredProbeKeepsHeldLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    holderDb, holderErr := sql.Open("mysql", dsn)
    if nil != holderErr {
        t.Fatalf("open holder: %v", holderErr)
    }
    defer holderDb.Close()

    competitorDb, competitorErr := sql.Open("mysql", dsn)
    if nil != competitorErr {
        t.Fatalf("open competitor: %v", competitorErr)
    }
    defer competitorDb.Close()

    name := "melody_lock_refresh_unanswered_probe"

    lock, isMysqlLock := NewLocker(bun.NewDB(holderDb, mysqldialect.New())).CreateLock(name, 0).(*mysqlLock)
    if false == isMysqlLock {
        t.Fatalf("expected a *mysqlLock")
    }

    competitor := NewLocker(bun.NewDB(competitorDb, mysqldialect.New())).CreateLock(name, 0)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }
    defer func() { _ = lock.Release(newLockRuntime()) }()

    holderBefore := observeLockHolder(t, competitorDb, name)
    if 0 == holderBefore {
        t.Fatalf("expected the lock to be held before the probe is poisoned")
    }

    poisonProbeSelect(t, lock)

    if refreshErr := lock.Refresh(newLockRuntime(), 0); nil != refreshErr {
        t.Fatalf("expected the refresh to answer held over an unanswered probe on a live session, got: %v", refreshErr)
    }

    if nil == lock.connection {
        t.Fatalf("the pinned connection was dropped for a session that is still alive")
    }

    holderAfter := observeLockHolder(t, competitorDb, name)
    if holderBefore != holderAfter {
        t.Fatalf(
            "the lock changed hands over an unanswered probe: session %d held it, session %d holds it now",
            holderBefore, holderAfter,
        )
    }

    contended, contendedErr := competitor.Acquire(newLockRuntime())
    if nil != contendedErr {
        t.Fatalf("competitor acquire: %v", contendedErr)
    }
    if true == contended {
        _ = competitor.Release(newLockRuntime())
        t.Fatalf("a second holder entered the exclusive section after one unanswered probe")
    }
}

/* The same distinction on the re-acquire path: a verify that could not be answered must not release
   the lock and report (false, nil), which told the caller it never held a lock it was holding. */
func TestMysqlLock_AcquireOnAnUnansweredVerifyKeepsHeldLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    holderDb, holderErr := sql.Open("mysql", dsn)
    if nil != holderErr {
        t.Fatalf("open holder: %v", holderErr)
    }
    defer holderDb.Close()

    competitorDb, competitorErr := sql.Open("mysql", dsn)
    if nil != competitorErr {
        t.Fatalf("open competitor: %v", competitorErr)
    }
    defer competitorDb.Close()

    name := "melody_lock_acquire_unanswered_verify"

    lock, isMysqlLock := NewLocker(bun.NewDB(holderDb, mysqldialect.New())).CreateLock(name, 0).(*mysqlLock)
    if false == isMysqlLock {
        t.Fatalf("expected a *mysqlLock")
    }

    competitor := NewLocker(bun.NewDB(competitorDb, mysqldialect.New())).CreateLock(name, 0)

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }
    defer func() { _ = lock.Release(newLockRuntime()) }()

    holderBefore := observeLockHolder(t, competitorDb, name)
    if 0 == holderBefore {
        t.Fatalf("expected the lock to be held before the verify is poisoned")
    }

    poisonProbeSelect(t, lock)

    reacquired, reacquireErr := lock.Acquire(newLockRuntime())
    if nil != reacquireErr {
        t.Fatalf("re-acquire over an unanswered verify: %v", reacquireErr)
    }
    if false == reacquired {
        t.Fatalf("the holder was told it does not hold a lock it is holding")
    }

    /* the session id is the observable, not the presence of a lock: a locker that released and took
       it again on a fresh session also ends up holding it, having opened a window a competitor could
       have walked through. Only an unchanged holder proves the lock was never let go. */
    holderAfter := observeLockHolder(t, competitorDb, name)
    if holderBefore != holderAfter {
        t.Fatalf(
            "the lock was released and retaken over an unanswered verify: session %d held it, session %d holds it now",
            holderBefore, holderAfter,
        )
    }

    contended, _ := competitor.Acquire(newLockRuntime())
    if true == contended {
        _ = competitor.Release(newLockRuntime())
        t.Fatalf("a second holder entered the exclusive section after one unanswered verify")
    }
}

/* The negative half, so the liveness branch above cannot pass by abstaining: a session that is
   genuinely gone HAS lost its lock, and the refresh must say so and drop the pin. */
func TestMysqlLock_RefreshOnADeadSessionReportsTheLockLost(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    holderDb, holderErr := sql.Open("mysql", dsn)
    if nil != holderErr {
        t.Fatalf("open holder: %v", holderErr)
    }
    defer holderDb.Close()

    observerDb, observerErr := sql.Open("mysql", dsn)
    if nil != observerErr {
        t.Fatalf("open observer: %v", observerErr)
    }
    defer observerDb.Close()

    name := "melody_lock_refresh_dead_session"

    lock, isMysqlLock := NewLocker(bun.NewDB(holderDb, mysqldialect.New())).CreateLock(name, 0).(*mysqlLock)
    if false == isMysqlLock {
        t.Fatalf("expected a *mysqlLock")
    }

    if acquired, acquireErr := lock.Acquire(newLockRuntime()); nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    var ownerId sql.NullInt64
    if ownerErr := observerDb.QueryRowContext(
        context.Background(), "SELECT IS_USED_LOCK(?)", name,
    ).Scan(&ownerId); nil != ownerErr {
        t.Fatalf("read lock owner: %v", ownerErr)
    }
    if false == ownerId.Valid {
        t.Fatalf("expected the lock to be held by a session")
    }

    if _, killErr := observerDb.ExecContext(
        context.Background(), "KILL "+strconv.FormatInt(ownerId.Int64, 10),
    ); nil != killErr {
        t.Logf("kill returned (tolerated): %v", killErr)
    }

    /* KILL is asynchronous: the server flags the thread and the session goes only once it notices */
    deadline := time.Now().Add(10 * time.Second)
    var refreshErr error
    for {
        refreshErr = lock.Refresh(newLockRuntime(), 0)
        if nil != refreshErr || true == time.Now().After(deadline) {
            break
        }

        time.Sleep(20 * time.Millisecond)
    }

    if nil == refreshErr {
        t.Fatalf("expected the refresh to report the lock lost once its session was killed")
    }
    if nil != lock.connection {
        t.Fatalf("the pin survived a session that is gone")
    }
}

/* every lock failure names both spellings: the caller's name and the folded form the server was actually asked for — a name past the limit is folded to a hash-suffixed form, and a diagnostic that showed only the caller's spelling sent the operator to look for a lock the server had never heard of */
func TestMysqlLock_FailuresNameTheFoldedLockNameBesideTheName(t *testing.T) {
    sqldb, openErr := sql.Open("mysql", "melody:melody@tcp(127.0.0.1:1)/melody")
    if nil != openErr {
        t.Fatal(openErr)
    }
    database := bun.NewDB(sqldb, mysqldialect.New())
    if closeErr := database.Close(); nil != closeErr {
        t.Fatal(closeErr)
    }

    name := strings.Repeat("melody:lock:", 8)
    lock := NewLocker(database).CreateLock(name, 0)

    _, acquireErr := lock.Acquire(newLockRuntime())
    if nil == acquireErr {
        t.Fatal("expected the acquire on a closed database to fail")
    }

    var typed *exception.Error
    if false == errors.As(acquireErr, &typed) {
        t.Fatalf("expected an exception.Error, got %T", acquireErr)
    }

    if name != typed.Context()["name"] || boundedLockName(name) != typed.Context()["lockName"] {
        t.Fatalf("expected the context to carry the name and its folded form, got %v", typed.Context())
    }

    if name == boundedLockName(name) {
        t.Fatal("control: the probe name must be long enough to be folded")
    }
}
