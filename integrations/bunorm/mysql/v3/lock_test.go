package mysql

import (
    "context"
    "database/sql"
    "os"
    "strconv"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    _ "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/v3/container"
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

func TestMysqlLock_AcquireVerifyErrorReleasesHeldLock(t *testing.T) {
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

    /* the verify now fails for real: the lock object must drop the dead connection and take the lock afresh. KILL is
       asynchronous — the server flags the thread and only releases its locks once that thread notices — so a single immediate
       attempt races the cleanup and reads GET_LOCK as 0 (held, no error). Retry until the dead session lets go. */
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

/* @info Mirrors TestMysqlLock_RefreshCanceledRuntimeContextKeepsHeldLock: a canceled request context is a transient caller-side condition, not a lost lock. Re-acquiring under one must not be mistaken for a verify error and must not RELEASE_LOCK a lock this process still holds. */
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

    /* @important KILL only flags the session; the GET_LOCK stays held until that session actually ends. Acquire probes with GET_LOCK(?, 0), which never waits, so the competitor below must not run before the kill has landed. */
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

/* @info A lock name longer than MySQL's 64-character user-level-lock limit would make GET_LOCK error on every Acquire, so RunExclusive fails closed forever and the wrapped job never runs. boundedLockName must fold such a name onto a form MySQL accepts. */
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
