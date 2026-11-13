package pgsql

import (
    "context"
    "database/sql"
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func pgLockDatabase(t *testing.T) *bun.DB {
    t.Helper()

    dsn := os.Getenv("POSTGRES_DSN")
    if "" == dsn {
        t.Skip("POSTGRES_DSN not set; skipping pgsql lock integration test")
    }

    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
    t.Cleanup(func() {
        sqldb.Close()
    })

    return bun.NewDB(sqldb, pgdialect.New())
}

func TestPgsqlLock_MutualExclusionAndRelease(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t), LockerConfig{})
    runtimeInstance := newLockRuntime()

    name := "melody_pg_lock_test"

    first := locker.CreateLock(name, bound.Duration{})
    second := locker.CreateLock(name, bound.Duration{})

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

func TestPgsqlLock_RefreshReportsLostLock(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t), LockerConfig{})
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_pg_lock_refresh_test", bound.Duration{})

    if refreshErr := lock.Refresh(runtimeInstance, bound.Duration{}); nil == refreshErr {
        t.Fatalf("expected refresh to fail before the lock is acquired")
    }

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, bound.Duration{}); nil != refreshErr {
        t.Fatalf("expected refresh to succeed while held: %v", refreshErr)
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, bound.Duration{}); nil == refreshErr {
        t.Fatalf("expected refresh to fail after release")
    }
}

/* a pinned session found dead by Refresh's probe is refused as a lost lock carrying the probe's failure as its cause, as the mysql twin carries its own. */
func TestPgsqlLock_RefreshCarriesTheProbeFailureOfALostLock(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t), LockerConfig{})
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_pg_lock_lost_cause_test", bound.Duration{})

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    pinned := lock.(*pgsqlLock)
    if closeErr := pinned.connection.Close(); nil != closeErr {
        t.Fatalf("close the pinned connection: %v", closeErr)
    }

    refreshErr := lock.Refresh(runtimeInstance, bound.Duration{})
    if nil == refreshErr {
        t.Fatalf("expected refresh to report the lost lock")
    }

    if false == errors.Is(refreshErr, sql.ErrConnDone) {
        t.Fatalf("expected the probe's failure carried as the cause, got %v", exception.LogContext(refreshErr, exception.LogContextConfig{}))
    }
}

/* every lock failure names both spellings: the caller's name and the advisory key halves the server was actually asked for, without which the operator has nothing to match against pg_locks */
func TestPgsqlLock_FailuresNameTheAdvisoryKeyBesideTheName(t *testing.T) {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://melody:melody@127.0.0.1:1/melody?sslmode=disable")))
    database := bun.NewDB(sqldb, pgdialect.New())
    if closeErr := database.Close(); nil != closeErr {
        t.Fatal(closeErr)
    }

    lock := NewLocker(database, LockerConfig{}).CreateLock("melody:very:long:lock:name", bound.Duration{})

    _, acquireErr := lock.Acquire(newLockRuntime())
    if nil == acquireErr {
        t.Fatal("expected the acquire on a closed database to fail")
    }

    var typed *exception.Error
    if false == errors.As(acquireErr, &typed) {
        t.Fatalf("expected an exception.Error, got %T", acquireErr)
    }

    keyHigh, keyLow := advisoryLockKey("melody:very:long:lock:name")
    if "melody:very:long:lock:name" != typed.Context()["name"] || keyHigh != typed.Context()["keyHigh"] || keyLow != typed.Context()["keyLow"] {
        t.Fatalf("expected the context to carry the name and both key halves, got %v", typed.Context())
    }
}

func TestPgsqlLock_ReacquiringAHeldLockKeepsItsSession(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t), LockerConfig{})
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_pg_lock_reacquire_test", bound.Duration{})
    defer func() {
        _ = lock.Release(runtimeInstance)
    }()

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    pinned := lock.(*pgsqlLock).connection

    reacquired, reacquireErr := lock.Acquire(runtimeInstance)
    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected a re-acquire of a held lock to succeed: %v %v", reacquired, reacquireErr)
    }

    if pinned != lock.(*pgsqlLock).connection {
        t.Fatalf("expected a re-acquire of a held lock to keep the session that holds it")
    }
}

/* a session the server ended took the advisory lock with it, so the next acquire discards the dead pin and takes the lock afresh on a new session, which then holds it against every other session */
func TestPgsqlLock_ReacquiringAfterTheSessionEndedTakesTheLockAfresh(t *testing.T) {
    database := pgLockDatabase(t)
    locker := NewLocker(database, LockerConfig{})
    runtimeInstance := newLockRuntime()

    name := "melody_pg_lock_reacquire_after_end_test"
    lock := locker.CreateLock(name, bound.Duration{})
    defer func() {
        _ = lock.Release(runtimeInstance)
    }()

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to succeed: %v %v", acquired, acquireErr)
    }

    pinned := lock.(*pgsqlLock).connection

    var backendPid int
    if pidErr := pinned.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&backendPid); nil != pidErr {
        t.Fatalf("read the pinned session's pid: %v", pidErr)
    }

    var terminated bool
    if terminateErr := database.DB.QueryRowContext(context.Background(), "SELECT pg_terminate_backend($1)", backendPid).Scan(&terminated); nil != terminateErr || false == terminated {
        t.Fatalf("end the pinned session: %v %v", terminated, terminateErr)
    }

    reacquired, reacquireErr := lock.Acquire(runtimeInstance)
    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected the lock to be taken afresh after its session ended: %v %v", reacquired, reacquireErr)
    }

    if pinned == lock.(*pgsqlLock).connection {
        t.Fatalf("expected the dead session's pin to be replaced")
    }

    contended, contendedErr := locker.CreateLock(name, bound.Duration{}).Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected the new session to hold the lock against another: %v %v", contended, contendedErr)
    }
}

func TestNewLocker_TheReleaseTimeoutReachesEveryLockAndAnAbsentOneTakesTheDefault(t *testing.T) {
    database := bun.NewDB(sql.OpenDB(pgdriver.NewConnector()), pgdialect.New())
    t.Cleanup(func() {
        _ = database.Close()
    })

    configured := NewLocker(database, LockerConfig{ReleaseTimeout: bound.Of(750 * time.Millisecond)}).CreateLock("melody_pg_lock_timeout_test", bound.Duration{}).(*pgsqlLock)
    if 750*time.Millisecond != configured.releaseTimeout {
        t.Fatalf("expected the configured release timeout on the lock, got %s", configured.releaseTimeout)
    }

    defaulted := NewLocker(database, LockerConfig{}).CreateLock("melody_pg_lock_timeout_test", bound.Duration{}).(*pgsqlLock)
    if 5*time.Second != defaulted.releaseTimeout {
        t.Fatalf("expected the default release timeout 5s, got %s", defaulted.releaseTimeout)
    }
}

func TestPgsqlLock_AnUnlimitedTtlAcquiresAndRefreshes(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t), LockerConfig{})
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_pg_lock_unlimited", bound.UnlimitedDuration())
    if acquired, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected an unlimited ttl to acquire: %v %v", acquired, acquireErr)
    }
    defer lock.Release(runtimeInstance)

    if refreshErr := lock.Refresh(runtimeInstance, bound.UnlimitedDuration()); nil != refreshErr {
        t.Fatalf("expected an unlimited refresh to probe the held lock: %v", refreshErr)
    }
}

func newReleaseRuntime(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

func acquireOnTheRecordingDouble(t *testing.T, connector *releaseRecordingConnector, config LockerConfig) (*pgsqlLock, int) {
    t.Helper()

    lock := NewLocker(newReleaseRecordingDatabase(t, connector), config).CreateLock("melody_release_budget", bound.Duration{}).(*pgsqlLock)

    acquired, acquireErr := lock.Acquire(newReleaseRuntime(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the double to grant the lock: %v %v", acquired, acquireErr)
    }

    connects, _, _ := connector.counts()

    return lock, connects
}

func TestPgsqlLock_ReleaseEndsAtTheCallersEarlierDeadlineAndEndsTheSession(t *testing.T) {
    connector := &releaseRecordingConnector{stallRelease: true}
    lock, connectsAfterAcquire := acquireOnTheRecordingDouble(t, connector, LockerConfig{ReleaseTimeout: bound.Of(5 * time.Second)})

    callerContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancel()

    startedAt := time.Now()
    releaseErr := lock.Release(newReleaseRuntime(callerContext))
    elapsed := time.Since(startedAt)

    if false == errors.Is(releaseErr, context.DeadlineExceeded) {
        t.Fatalf("expected the release cut by the caller's deadline, got %v", releaseErr)
    }

    if time.Second <= elapsed {
        t.Fatalf("expected the release to end at the caller's 20ms deadline, it took %s", elapsed)
    }

    if "5s" != exception.LogContext(releaseErr, exception.LogContextConfig{})["releaseTimeout"] {
        t.Fatalf("expected the failure to name the locker's budget beside the deadline, got %v", exception.LogContext(releaseErr, exception.LogContextConfig{}))
    }

    if _, closes, _ := connector.counts(); 1 != closes {
        t.Fatalf("expected the session the release was cut on to be ended, got %d closes", closes)
    }

    connector.mutex.Lock()
    connector.stallRelease = false
    connector.mutex.Unlock()

    if _, acquireErr := lock.Acquire(newReleaseRuntime(context.Background())); nil != acquireErr {
        t.Fatalf("expected a second acquire: %v", acquireErr)
    }

    if connects, _, _ := connector.counts(); connectsAfterAcquire+1 != connects {
        t.Fatalf("expected the second acquire to dial a new session, got %d dials after %d", connects, connectsAfterAcquire)
    }
}

func TestPgsqlLock_ReleaseEndsAtTheLockersEarlierBudget(t *testing.T) {
    connector := &releaseRecordingConnector{stallRelease: true}
    lock, _ := acquireOnTheRecordingDouble(t, connector, LockerConfig{ReleaseTimeout: bound.Of(20 * time.Millisecond)})

    callerContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    startedAt := time.Now()
    releaseErr := lock.Release(newReleaseRuntime(callerContext))
    elapsed := time.Since(startedAt)

    if false == errors.Is(releaseErr, context.DeadlineExceeded) || time.Second <= elapsed {
        t.Fatalf("expected the release cut by the locker's 20ms budget, got %v after %s", releaseErr, elapsed)
    }

    if _, closes, _ := connector.counts(); 1 != closes {
        t.Fatalf("expected the session the release was cut on to be ended, got %d closes", closes)
    }
}

func TestPgsqlLock_ACancelledCallerStillReleases(t *testing.T) {
    connector := &releaseRecordingConnector{}
    lock, _ := acquireOnTheRecordingDouble(t, connector, LockerConfig{})

    callerContext, cancel := context.WithCancel(context.Background())
    cancel()

    if releaseErr := lock.Release(newReleaseRuntime(callerContext)); nil != releaseErr {
        t.Fatalf("expected the release to run past the caller's cancellation, got %v", releaseErr)
    }

    connector.mutex.Lock()
    releases, releaseContextErr := connector.releases, connector.releaseContextErr
    connector.mutex.Unlock()

    if 1 != releases || nil != releaseContextErr {
        t.Fatalf("expected one release on a live context, got %d releases over %v", releases, releaseContextErr)
    }
}

func TestPgsqlLock_ASuccessfulReleaseReturnsTheSessionToThePool(t *testing.T) {
    connector := &releaseRecordingConnector{}
    lock, connectsAfterAcquire := acquireOnTheRecordingDouble(t, connector, LockerConfig{})

    if releaseErr := lock.Release(newReleaseRuntime(context.Background())); nil != releaseErr {
        t.Fatalf("expected the release to succeed, got %v", releaseErr)
    }

    if _, acquireErr := lock.Acquire(newReleaseRuntime(context.Background())); nil != acquireErr {
        t.Fatalf("expected a second acquire: %v", acquireErr)
    }

    if connects, closes, _ := connector.counts(); connectsAfterAcquire != connects || 0 != closes {
        t.Fatalf("expected the released session reused, got %d dials after %d and %d closes", connects, connectsAfterAcquire, closes)
    }
}

func TestNewLocker_RefusesAReleaseTimeoutOutsideItsRuleNamingIt(t *testing.T) {
    for _, releaseTimeout := range []bound.Duration{bound.Of(time.Duration(0)), bound.Of(-time.Second), bound.UnlimitedDuration()} {
        expectPanicNamingField(t, "LockerConfig.ReleaseTimeout", func() {
            NewLocker(newReleaseRecordingDatabase(t, &releaseRecordingConnector{}), LockerConfig{ReleaseTimeout: releaseTimeout})
        })
    }
}

func TestPgsqlLock_ACallerDeadlineAlreadyPassedReleasesOnTheLockersBudget(t *testing.T) {
    connector := &releaseRecordingConnector{}
    lock, _ := acquireOnTheRecordingDouble(t, connector, LockerConfig{})

    callerContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if releaseErr := lock.Release(newReleaseRuntime(callerContext)); nil != releaseErr {
        t.Fatalf("expected the release to run on the locker's budget, got %v", releaseErr)
    }

    if _, closes, releases := connector.counts(); 1 != releases || 0 != closes {
        t.Fatalf("expected one release and the session returned to the pool, got %d releases and %d closes", releases, closes)
    }
}

/* typedNilRuntime is a runtime implementation held as a nil pointer, which passes a plain nil comparison and panics on its first call */
type typedNilRuntime struct {
    runtimecontract.Runtime
}

func TestPgsqlLock_ANilOrTypedNilRuntimeReleasesOnTheLockersBudget(t *testing.T) {
    var typedNil *typedNilRuntime

    for _, runtimeInstance := range []runtimecontract.Runtime{nil, typedNil} {
        connector := &releaseRecordingConnector{}
        lock, _ := acquireOnTheRecordingDouble(t, connector, LockerConfig{})

        if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
            t.Fatalf("expected the release to run on the locker's budget, got %v", releaseErr)
        }

        if _, closes, releases := connector.counts(); 1 != releases || 0 != closes {
            t.Fatalf("expected one release and the session returned to the pool, got %d releases and %d closes", releases, closes)
        }
    }
}

func TestLocker_DeclaresNoLeaseMarkSoASessionGateIsAdmitted(t *testing.T) {
    if _, isLeaseLocker := any(&Locker{}).(lockcontract.LeaseLocker); true == isLeaseLocker {
        t.Fatalf("expected a session-style locker not to declare the lease mark")
    }
}

func TestPgsqlLock_AnUnlockTheServerAnswersFalseIsAnErrorAndEndsTheSession(t *testing.T) {
    connector := &releaseRecordingConnector{unlockAnswersFalse: true}
    lock, _ := acquireOnTheRecordingDouble(t, connector, LockerConfig{})

    releaseErr := lock.Release(newReleaseRuntime(context.Background()))
    if nil == releaseErr || nil == errors.Unwrap(releaseErr) || false == strings.Contains(errors.Unwrap(releaseErr).Error(), "does not hold the lock") {
        t.Fatalf("expected the release refused as a lock this session does not hold, got %v", releaseErr)
    }

    if _, closes, releases := connector.counts(); 1 != releases || 1 != closes {
        t.Fatalf("expected one unlock and the session ended, got %d unlocks and %d closes", releases, closes)
    }
}
