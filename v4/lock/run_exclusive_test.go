package lock

import (
    "context"
    "errors"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func testRuntimeWithContext(ctx context.Context) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(ctx, serviceContainer.NewScope())
}

type acquireFailingLocker struct{}

func (instance *acquireFailingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &acquireFailingLock{}, nil
}

type acquireFailingLock struct{}

func (instance *acquireFailingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return false, exception.NewError("store unreachable", nil, nil)
}

func (instance *acquireFailingLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *acquireFailingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

type refreshFailingLocker struct {
    inner lockcontract.Locker
}

func (instance *refreshFailingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &refreshFailingLock{inner: mustCreateLock(instance.inner.CreateLock(name, options))}, nil
}

type refreshFailingLock struct {
    inner lockcontract.Lock
}

func (instance *refreshFailingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *refreshFailingLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *refreshFailingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return exception.NewError("lease lost", nil, nil)
}

/* contextCancelledAcquireLock stands in for a backend round trip cut short by a SIGTERM: Acquire fails with the runtime cancellation wrapped in a store error, as a real locker reports an in-flight Acquire whose context is cancelled. */
type contextCancelledAcquireLocker struct{}

func (instance *contextCancelledAcquireLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &contextCancelledAcquireLock{}, nil
}

type contextCancelledAcquireLock struct{}

func (instance *contextCancelledAcquireLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return false, exception.NewError("acquire aborted", nil, runtimeInstance.Context().Err())
}

func (instance *contextCancelledAcquireLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *contextCancelledAcquireLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

func TestRunExclusive_RunsAndReleases(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    ranFirst := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:tick", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        ranFirst = true
        return nil
    })
    if nil != runErr || false == ran || false == ranFirst {
        t.Fatalf("expected first run to execute: ran=%v err=%v", ran, runErr)
    }

    /* the lock must be released right after the run, not after the ttl */
    ranSecond := false
    ran, runErr = RunExclusive(runtimeInstance, locker, "cron:tick", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        ranSecond = true
        return nil
    })
    if nil != runErr || false == ran || false == ranSecond {
        t.Fatalf("expected second run to execute after release: ran=%v err=%v", ran, runErr)
    }
}

func TestRunExclusive_SkipsWhenHeldByAnotherHolder(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    holder := mustCreateLock(locker.CreateLock("cron:held", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire")
    }

    called := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:held", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        called = true
        return nil
    })
    if nil != runErr {
        t.Fatalf("unexpected error when skipping: %v", runErr)
    }

    if true == ran || true == called {
        t.Fatalf("expected the run to be skipped while held: ran=%v called=%v", ran, called)
    }
}

func TestRunExclusive_FailsClosedOnAcquireError(t *testing.T) {
    runtimeInstance := testRuntime()

    called := false
    ran, runErr := RunExclusive(runtimeInstance, &acquireFailingLocker{}, "cron:error", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        called = true
        return nil
    })
    if nil == runErr {
        t.Fatalf("expected an acquire error to surface")
    }

    if true == ran || true == called {
        t.Fatalf("expected no run on acquire error: ran=%v called=%v", ran, called)
    }
}

func TestRunExclusive_AcquireCancellationIsShutdownNotError(t *testing.T) {
    /* a SIGTERM cancels the context the backend is called with, so an in-flight Acquire fails with the cancellation; that is the stop itself and reads as a clean skip, not a failed run */
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := testRuntimeWithContext(cancelledContext)

    called := false
    ran, runErr := RunExclusive(runtimeInstance, &contextCancelledAcquireLocker{}, "cron:sigterm", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        called = true
        return nil
    })
    if nil != runErr {
        t.Fatalf("an acquire cancelled by shutdown must not surface as an error: %v", runErr)
    }

    if true == ran || true == called {
        t.Fatalf("expected a clean skip on a shutdown-cancelled acquire: ran=%v called=%v", ran, called)
    }
}

func TestRunExclusive_PropagatesFnError(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    runtimeInstance := testRuntime()

    expected := exception.NewError("work failed", nil, nil)
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:failing", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        return expected
    })
    if false == ran {
        t.Fatalf("expected the run to execute")
    }

    if expected != runErr {
        t.Fatalf("expected the callback error to propagate, got: %v", runErr)
    }
}

func TestRunExclusive_RefreshFailureCancelsChildRuntime(t *testing.T) {
    locker := &refreshFailingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    runtimeInstance := testRuntime()

    cancelled := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:lease", ExclusiveOptions{Ttl: bound.Of(20 * time.Millisecond)}, func(childRuntime containercontract.Runtime) error {
        select {
        case <-childRuntime.Context().Done():
            cancelled = true
        case <-time.After(5 * time.Second):
        }

        return nil
    })
    if false == ran {
        t.Fatalf("expected the run to start")
    }

    if false == cancelled {
        t.Fatalf("expected the child runtime to be cancelled on refresh failure")
    }

    if nil == runErr {
        t.Fatalf("expected the lost lease to surface as an error")
    }
}

func TestRunExclusive_ReleasesEvenWhenCallerContextIsCancelled(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := testRuntimeWithContext(cancelledContext)

    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:sigterm", ExclusiveOptions{Ttl: bound.Of(time.Minute)}, func(childRuntime containercontract.Runtime) error {
        return nil
    })
    if nil != runErr || false == ran {
        t.Fatalf("expected the run to execute: ran=%v err=%v", ran, runErr)
    }

    /* the release must have gone through on the detached runtime despite the cancelled caller context */
    contender := mustCreateLock(locker.CreateLock("cron:sigterm", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
    acquired, _ := contender.Acquire(testRuntime())
    if false == acquired {
        t.Fatalf("expected the lock to be released despite the cancelled caller context")
    }
}

/* recordingRefreshLocker captures the ttl each Refresh is asked to write, so a test can assert the lease margin the refresher gives a lease-style backend. */
type recordingRefreshLocker struct {
    inner      lockcontract.Locker
    refreshTtl chan bound.Duration
}

func (instance *recordingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &recordingRefreshLock{inner: mustCreateLock(instance.inner.CreateLock(name, options)), refreshTtl: instance.refreshTtl}, nil
}

type recordingRefreshLock struct {
    inner      lockcontract.Lock
    refreshTtl chan bound.Duration
}

func (instance *recordingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *recordingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *recordingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    select {
    case instance.refreshTtl <- options.Ttl:
    default:
    }

    return instance.inner.Refresh(runtimeInstance, options)
}

/* blockingRefreshLock blocks inside Refresh until its runtime context is cancelled, standing in for a backend whose connection has been blackholed by a network partition. */
type blockingRefreshLocker struct {
    entered chan struct{}
}

func (instance *blockingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &blockingRefreshLock{entered: instance.entered}, nil
}

type blockingRefreshLock struct {
    entered chan struct{}
    closed  bool
}

func (instance *blockingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *blockingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *blockingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if false == instance.closed {
        instance.closed = true
        close(instance.entered)
    }

    <-runtimeInstance.Context().Done()

    return exception.NewError("refresh aborted", nil, runtimeInstance.Context().Err())
}

/* A lease-style backend rewrites the lease to now+ttl on Refresh. Handing it the probe interval itself would renew the lease exactly as it expires, so every probe races its own expiry: callback is cancelled spuriously and the lapsed lease lets a second instance run alongside it. The probe must renew for a multiple of its own cadence, and the derived interval must never reach time.NewTicker as zero. */
func TestResolveRefreshSchedule(t *testing.T) {
    for _, testCase := range []struct {
        name             string
        ttl              bound.Resolved[time.Duration]
        wantedInterval   time.Duration
        wantedRefreshTtl time.Duration
    }{
        {"a lease ttl refreshes at half of it", bound.Resolved[time.Duration]{Value: 30 * time.Second}, 15 * time.Second, 30 * time.Second},
        {"an unlimited ttl probes with a lease margin", bound.Resolved[time.Duration]{Unlimited: true}, defaultSessionProbeInterval, sessionProbeTtlFactor * defaultSessionProbeInterval},
        {"the 2ms minimum refreshes at the 1ms floor", bound.Resolved[time.Duration]{Value: 2 * time.Millisecond}, minimumRefreshInterval, 2 * time.Millisecond},
        {"a tiny ttl is floored off zero", bound.Resolved[time.Duration]{Value: 1 * time.Nanosecond}, minimumRefreshInterval, 1 * time.Nanosecond},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            interval, refreshTtl := resolveRefreshSchedule(testCase.ttl, defaultSessionProbeInterval)

            if testCase.wantedInterval != interval {
                t.Fatalf("interval: wanted %s, got %s", testCase.wantedInterval, interval)
            }
            if testCase.wantedRefreshTtl != refreshTtl {
                t.Fatalf("refresh ttl: wanted %s, got %s", testCase.wantedRefreshTtl, refreshTtl)
            }
            if 0 >= interval {
                t.Fatal("a non-positive interval would panic time.NewTicker")
            }

            /* a ttl below the floor is a misconfiguration the lock cannot rescue — the lease expires before any renewal can land — but it must not panic; every sane ttl renews with slack to spare */
            if minimumRefreshInterval <= testCase.ttl.Value || true == testCase.ttl.Unlimited {
                if 0 >= refreshTtl-interval {
                    t.Fatalf("the refresh ttl %s gives no margin over the %s cadence", refreshTtl, interval)
                }
            }
        })
    }
}

func TestRunExclusive_ATtlBelowTheMinimumIsRefusedBeforeTheLockIsCreated(t *testing.T) {
    for _, refusedTtl := range []bound.Duration{bound.Of(time.Microsecond), bound.Of(2*time.Millisecond - time.Microsecond)} {
        acquired, runErr := RunExclusive(
            testRuntimeWithContext(context.Background()),
            &createLockForbiddenLocker{t: t},
            "tiny-ttl",
            ExclusiveOptions{Ttl: refusedTtl},
            func(containercontract.Runtime) error {
                t.Fatal("the callback ran under a refused ttl")

                return nil
            },
        )

        refusal, isMelodyErr := runErr.(*exception.Error)
        if true == acquired || false == isMelodyErr || "ExclusiveOptions.Ttl" != refusal.Context()["field"] {
            t.Fatalf("expected %s to be refused naming ExclusiveOptions.Ttl, got acquired=%v err=%v", refusedTtl, acquired, runErr)
        }
    }
}

func TestRunExclusive_TheMinimumLeaseRenewsBeforeItsDemotionInstant(t *testing.T) {
    acquiredAt := time.Unix(1000, 0)

    for _, testCase := range []struct {
        ttl              time.Duration
        renewsBeforeLoss bool
    }{
        {minimumLeaseTtl, true},
        {minimumRefreshInterval, false},
    } {
        interval, refreshTtl := resolveRefreshSchedule(bound.Resolved[time.Duration]{Value: testCase.ttl}, defaultSessionProbeInterval)

        firstTick := acquiredAt.Add(interval)
        demotionAt := leaseDemotionAt(acquiredAt.Add(refreshTtl), interval)

        if testCase.renewsBeforeLoss != firstTick.Before(demotionAt) {
            t.Fatalf("a %s lease: first renewal tick at +%s, demotion instant at +%s", testCase.ttl, firstTick.Sub(acquiredAt), demotionAt.Sub(acquiredAt))
        }
    }
}

/* createLockForbiddenLocker fails the test when a lock is created, for the refusals that must land before the locker is asked. */
type createLockForbiddenLocker struct {
    t *testing.T
}

func (instance *createLockForbiddenLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    instance.t.Fatalf("a lock was created for %q under a refused configuration", name)

    return nil, nil
}

/* waitForRefreshInFlight blocks until the blocking locker's Refresh has been entered. A loaded runner can schedule the refresh goroutine past the demotion instant, which then demotes without ever calling Refresh; the child runtime's cancellation ends the wait with an error instead of parking the test until the package timeout. */
func waitForRefreshInFlight(childRuntime containercontract.Runtime, entered <-chan struct{}) error {
    select {
    case <-entered:
        return nil
    case <-childRuntime.Context().Done():
        return exception.NewError("the run was demoted before any refresh was in flight", nil, nil)
    }
}

/* A Refresh already blocked on an unresponsive backend when callback returns must be interrupted: closing refreshDone alone cannot reach it, so RunExclusive would wait on the goroutine forever while holding the lock. Cancelling the renewal's context before Wait unblocks it, and the resulting error must read as the run's end, not as a lost lease. The ttl leaves the refresh, entered at half of it, a quarter of it before the demotion instant, wide enough for callback to return on a loaded runner. */
func TestRunExclusive_DoesNotHangWhenARefreshIsInFlightAtReturn(t *testing.T) {
    entered := make(chan struct{})
    locker := &blockingRefreshLocker{entered: entered}

    done := make(chan struct{})

    go func() {
        defer close(done)

        _, runErr := RunExclusive(
            testRuntimeWithContext(context.Background()),
            locker,
            "blocking",
            ExclusiveOptions{Ttl: bound.Of(1 * time.Second)},
            func(childRuntime containercontract.Runtime) error {
                return waitForRefreshInFlight(childRuntime, entered)
            },
        )

        if nil != runErr {
            t.Errorf("expected a clean run, got %v", runErr)
        }
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("RunExclusive hung with a refresh in flight at callback return")
    }
}

/* A SIGTERM does not reach the refresh loop: the refresh in flight stays blocked until callback returns and the join cancels it. That cancellation is the run's end, not a lost lease: reporting it would turn every graceful stop of a long-running exclusive command into an error a cron fleet reads as a failed run. The ttl leaves the refresh, entered at half of it, a quarter of it before the demotion instant, wide enough for callback to cancel the parent on a loaded runner. */
func TestRunExclusive_ContextCancellationIsShutdownNotLostLease(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    runtimeInstance := testRuntimeWithContext(parentContext)
    entered := make(chan struct{})
    locker := &blockingRefreshLocker{entered: entered}

    ran, err := RunExclusive(
        runtimeInstance,
        locker,
        "shutdown",
        ExclusiveOptions{Ttl: bound.Of(1 * time.Second)},
        func(childRuntime containercontract.Runtime) error {
            /* wait until a refresh is genuinely in flight, then shut the process down underneath it */
            if waitErr := waitForRefreshInFlight(childRuntime, entered); nil != waitErr {
                return waitErr
            }
            cancelParent()

            <-childRuntime.Context().Done()

            /* let the refresh goroutine record its cancelled-call failure before callback returns and closes the done channel */
            time.Sleep(50 * time.Millisecond)

            return nil
        },
    )

    if false == ran {
        t.Fatalf("the holder must report that it ran")
    }
    if nil != err {
        t.Fatalf("a cancelled refresh during shutdown must not be reported as a lost lease: %v", err)
    }
}

/* The holder has the same shape as the leader gate: a renewal that never answers returns no error, so nothing cancels the child runtime and the callback keeps working while the lease lapses and another instance acquires — two runs of the same exclusive work at once. The renewal deadline is what turns the silence into the cancellation the callback is written to obey. */
func TestRunExclusive_ARenewalThatNeverAnswersStopsTheCallback(t *testing.T) {
    locker := &hangingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    ttl := 400 * time.Millisecond
    cancelled := false

    ran, err := RunExclusive(
        testRuntimeWithContext(runContext),
        locker,
        "job:hanging-refresh",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(childRuntime containercontract.Runtime) error {
            select {
            case <-childRuntime.Context().Done():
                cancelled = true

                return nil
            case <-time.After(8 * ttl):
                /* far past the lease: whoever acquired it in the meantime is running the same work */
                return nil
            }
        },
    )

    if false == ran {
        t.Fatalf("the holder must report that it ran")
    }

    if false == cancelled {
        t.Fatalf("the callback kept working past the lease while the renewal never answered")
    }

    if nil == err {
        t.Fatalf("a renewal that never answered must be reported as a lost lease")
    }
}

/* slowSucceedingRefreshLock answers every renewal successfully, but only after a delay — the shape of a store that is under load rather than gone. */
type slowSucceedingRefreshLocker struct {
    delay time.Duration
}

func (instance *slowSucceedingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &slowSucceedingRefreshLock{delay: instance.delay}, nil
}

type slowSucceedingRefreshLock struct {
    delay time.Duration
}

func (instance *slowSucceedingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *slowSucceedingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *slowSucceedingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    select {
    case <-time.After(instance.delay):
        return nil
    case <-runtimeInstance.Context().Done():
        return runtimeInstance.Context().Err()
    }
}

/* a renewal that lands before the lease's demotion instant renewed it, however slowly; the delay sits between an eighth and a quarter of the ttl */
func TestRunExclusive_ASlowButSuccessfulRenewalDoesNotLoseTheLease(t *testing.T) {
    ttl := 400 * time.Millisecond
    locker := &slowSucceedingRefreshLocker{delay: 60 * time.Millisecond}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:slow-store",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(runtimeInstance containercontract.Runtime) error {
            time.Sleep(time.Second)
            completed = true

            return nil
        },
    )

    if false == ran {
        t.Fatal("expected the exclusive run to have taken the lock")
    }

    if nil != runErr {
        t.Fatalf("expected a slow but successful renewal to keep the lease, got %v", runErr)
    }

    if false == completed {
        t.Fatal("the callback was cancelled: a renewal that landed before the demotion instant was read as a lost lock")
    }
}

/* a renewal still unanswered at the lease's demotion instant, a quarter of the ttl before the lapse, is cut there and costs the lease, since whether it would land before a contender acquires cannot be known */
func TestRunExclusive_ARenewalThatCannotLandBeforeTheDemotionInstantLosesTheLease(t *testing.T) {
    ttl := 400 * time.Millisecond
    locker := &slowSucceedingRefreshLocker{delay: 150 * time.Millisecond}

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:slower-store",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(runtimeInstance containercontract.Runtime) error {
            select {
            case <-runtimeInstance.Context().Done():
                return nil
            case <-time.After(5 * time.Second):
                return nil
            }
        },
    )

    if false == ran {
        t.Fatal("expected the exclusive run to have taken the lock")
    }

    if nil == runErr {
        t.Fatal("expected a renewal that could not land before the demotion instant to lose the lease")
    }
}

/* the other half of the invariant: a renewal that keeps failing must still demote, and must do so before the lease it last wrote can be acquired by anyone else rather than after. */
func TestRunExclusive_APersistentlyFailingRenewalStillLosesTheLease(t *testing.T) {
    ttl := 200 * time.Millisecond
    locker := &refreshFailingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:dead-store",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(runtimeInstance containercontract.Runtime) error {
            select {
            case <-runtimeInstance.Context().Done():
                return runtimeInstance.Context().Err()
            case <-time.After(5 * time.Second):
                completed = true

                return nil
            }
        },
    )

    if false == ran {
        t.Fatal("expected the exclusive run to have taken the lock")
    }

    if nil == runErr {
        t.Fatal("expected a renewal that never succeeds to lose the lease")
    }

    if true == completed {
        t.Fatal("expected the callback to be cancelled once the lease could no longer be saved")
    }
}

/* intermittentRefreshLock fails the first renewal and answers every one after it, which is what a store that dropped a connection and reconnected looks like from here. */
type intermittentRefreshLocker struct{}

func (instance *intermittentRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &intermittentRefreshLock{}, nil
}

type intermittentRefreshLock struct {
    attempts atomic.Int64
}

func (instance *intermittentRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *intermittentRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *intermittentRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if 1 == instance.attempts.Add(1) {
        return exception.NewError("refresh dropped by the store", nil, nil)
    }

    return nil
}

/* One failed renewal is not a lost lease. The renewal cadence is half the ttl precisely so that a lost attempt still leaves a whole second attempt before the lease lapses; a policy that demotes on the first failure throws that margin away and turns every dropped connection into cancelled work and, under a LeaderGate, a dropped term. What must demote is the lease clock — the attempt after the failure lands, rewrites the lease, and nothing was ever in danger. */
func TestRunExclusive_ASingleFailedRenewalIsSurvivedByTheNextOne(t *testing.T) {
    ttl := 200 * time.Millisecond
    locker := &intermittentRefreshLocker{}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:flaky-store",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(runtimeInstance containercontract.Runtime) error {
            time.Sleep(500 * time.Millisecond)
            completed = true

            return nil
        },
    )

    if false == ran {
        t.Fatal("expected the exclusive run to have taken the lock")
    }

    if nil != runErr {
        t.Fatalf("expected one dropped renewal to be survived by the next, got %v", runErr)
    }

    if false == completed {
        t.Fatal("the callback was cancelled: a single dropped renewal was read as a lost lock, throwing away the whole point of renewing at half the ttl")
    }
}

/* slowAcquireCountingLocker answers Acquire late — the shape of a degraded store whose reply crawls back — and fails every renewal, counting them. */
type slowAcquireCountingLocker struct {
    acquireDelay time.Duration
    refreshCount atomic.Int64
}

func (instance *slowAcquireCountingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &slowAcquireCountingLock{locker: instance}, nil
}

type slowAcquireCountingLock struct {
    locker *slowAcquireCountingLocker
}

func (instance *slowAcquireCountingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    time.Sleep(instance.locker.acquireDelay)
    return true, nil
}

func (instance *slowAcquireCountingLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *slowAcquireCountingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    instance.locker.refreshCount.Add(1)
    return exception.NewError("lease lost", nil, nil)
}

func TestRunExclusive_LeaseIsDatedFromTheAcquireIssueInstant(t *testing.T) {
    /* the acquire answers 600ms after it is issued, against a 400ms ttl: the lease the store wrote has already lapsed by the time callback starts. Dated from the ISSUE instant, its demotion instant has passed and the caller is demoted before any renewal; dated from the ANSWER, the believed lease would run a further 600ms past the real one, renewals would be attempted and a failure read as survivable, and the callback would keep running through a window in which a second instance can legally acquire. The refresh count at demotion is the observable that separates the two datings. */
    locker := &slowAcquireCountingLocker{acquireDelay: 600 * time.Millisecond}

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "job:slow-acquire",
        ExclusiveOptions{Ttl: bound.Of(400 * time.Millisecond)},
        func(childRuntime containercontract.Runtime) error {
            select {
            case <-childRuntime.Context().Done():
                return nil
            case <-time.After(5 * time.Second):
                return exception.NewError("callback was never cancelled", nil, nil)
            }
        },
    )

    if false == ran {
        t.Fatalf("expected the callback to run")
    }

    if nil == runErr {
        t.Fatalf("expected the lost lease to surface as an error")
    }

    if 0 != locker.refreshCount.Load() {
        t.Fatalf("expected a lease dated from the acquire issue instant to be demoted before any renewal, got %d renewals", locker.refreshCount.Load())
    }
}

/* releaseFailingLocker acquires and refreshes cleanly and fails every release — the shape of a store hiccup exactly at release time. */
type releaseFailingLocker struct{}

func (instance *releaseFailingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &releaseFailingLock{}, nil
}

type releaseFailingLock struct{}

func (instance *releaseFailingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *releaseFailingLock) Release(runtimeInstance containercontract.Runtime) error {
    return exception.NewError("store unreachable at release", nil, nil)
}

func (instance *releaseFailingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

func TestRunExclusive_FailedReleaseIsLogged(t *testing.T) {
    runtimeInstance, logger := runtimeWithRecordingLogger(context.Background())

    ran, runErr := RunExclusive(
        runtimeInstance,
        &releaseFailingLocker{},
        "job:release-fails",
        ExclusiveOptions{Ttl: bound.Of(time.Minute)},
        func(childRuntime containercontract.Runtime) error {
            return nil
        },
    )

    if false == ran || nil != runErr {
        t.Fatalf("expected a clean run, got ran=%v err=%v", ran, runErr)
    }

    /* the run itself stays green — the work happened — but the stranded lock must name itself: without this record every peer's next tick skips with a log blaming a run that is not happening, and the operator cannot tell a failed release from a crash */
    if false == logger.hasMessageContaining("lock release failed") {
        t.Fatalf("expected the failed release to be logged")
    }
}

func TestRunExclusive_LostLeaseKeepsTheCallbackErrorInTheChain(t *testing.T) {
    callbackErr := exception.NewError("callback failed for its own reason", nil, nil)

    _, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        &refreshFailingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})},
        "job:joined-cause",
        ExclusiveOptions{Ttl: bound.Of(100 * time.Millisecond)},
        func(childRuntime containercontract.Runtime) error {
            <-childRuntime.Context().Done()
            return callbackErr
        },
    )

    if nil == runErr {
        t.Fatalf("expected the lost lease to surface as an error")
    }

    /* flattened to a string — the defect — the callback's own failure lost its identity for errors.Is at the process boundary; joined, both the refresh failure and the callback error answer */
    if false == errors.Is(runErr, callbackErr) {
        t.Fatalf("expected the callback error to stay reachable through errors.Is, got %v", runErr)
    }
}

/* panickingRefreshLocker acquires cleanly and panics on every renewal — the shape of a backend defect surfacing on the refresh goroutine, which carries no recover of the caller's. */
type panickingRefreshLocker struct{}

func (instance *panickingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &panickingRefreshLock{}, nil
}

type panickingRefreshLock struct{}

func (instance *panickingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *panickingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

var errPanickingRefreshBackend = exception.NewError("backend exploded", nil, nil)

func (instance *panickingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    panic(errPanickingRefreshBackend)
}

func TestRunExclusive_PanickingRefreshDemotesInsteadOfKillingTheProcess(t *testing.T) {
    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        &panickingRefreshLocker{},
        "job:refresh-panics",
        ExclusiveOptions{Ttl: bound.Of(50 * time.Millisecond)},
        func(childRuntime containercontract.Runtime) error {
            select {
            case <-childRuntime.Context().Done():
                return nil
            case <-time.After(5 * time.Second):
                return exception.NewError("callback was never cancelled", nil, nil)
            }
        },
    )

    if false == ran {
        t.Fatalf("expected the callback to run")
    }

    if nil == runErr || false == chainContainsMessage(runErr, "lock refresh panicked") {
        t.Fatalf("expected the recovered refresh panic to surface as the lost-lease cause, got %v", runErr)
    }
    if false == errors.Is(runErr, errPanickingRefreshBackend) {
        t.Fatalf("expected the panic value as the cause of the refresh failure, got %v", runErr)
    }

    if panicStack := chainContextValue(runErr, "lock refresh panicked", "panicStack"); false == strings.Contains(panicStack, "panickingRefreshLock") {
        t.Fatalf("expected the stack the refresh panicked on, got %q", panicStack)
    }
}

/* chainContextValue answers a context value of the first error of the chain, joined branches included, whose message holds the fragment */
func chainContextValue(err error, fragment string, key string) string {
    if nil == err {
        return ""
    }

    if melodyErr, isMelodyErr := err.(*exception.Error); true == isMelodyErr && true == strings.Contains(melodyErr.Message(), fragment) {
        value, _ := melodyErr.Context()[key].(string)

        return value
    }

    if joined, isJoined := err.(interface{ Unwrap() []error }); true == isJoined {
        for _, branch := range joined.Unwrap() {
            if value := chainContextValue(branch, fragment, key); "" != value {
                return value
            }
        }

        return ""
    }

    return chainContextValue(errors.Unwrap(err), fragment, key)
}

func TestLeaseDemotionClockRenew_ASuccessAnsweredPastTheDemotionInstantDemotes(t *testing.T) {
    testCases := []struct {
        name            string
        instantPassed   bool
        settled         bool
        expectedDemoted bool
    }{
        {name: "past the instant", instantPassed: true, settled: false, expectedDemoted: true},
        {name: "ahead of the instant", instantPassed: false, settled: false, expectedDemoted: false},
        {name: "past the instant of a run that ended", instantPassed: true, settled: true, expectedDemoted: false},
    }

    for _, testCase := range testCases {
        clock := newLeaseDemotionClock(true, true, time.Now().Add(time.Hour), time.Second, time.Second)

        if true == testCase.instantPassed {
            /* the instant moves without re-arming the timer, the window where the renewal answers before the timer fires */
            clock.leaseExpiry = time.Now().Add(-time.Hour)
        }

        demoteCalls := 0
        demoted, renewErr := clock.renew(
            func() error { return nil },
            func() { demoteCalls++ },
            func() bool { return testCase.settled },
        )
        clock.stop()

        if nil != renewErr {
            t.Fatalf("%s: unexpected error: %v", testCase.name, renewErr)
        }

        if testCase.expectedDemoted != demoted {
            t.Fatalf("%s: expected demoted %v, got %v", testCase.name, testCase.expectedDemoted, demoted)
        }

        expectedCalls := 0
        if true == testCase.expectedDemoted {
            expectedCalls = 1
        }

        if expectedCalls != demoteCalls {
            t.Fatalf("%s: expected %d demote calls, got %d", testCase.name, expectedCalls, demoteCalls)
        }
    }
}

/* chainContainsMessage walks the whole cause chain, joined branches included, so an assertion on a buried cause does not depend on how the top error renders. */
func chainContainsMessage(err error, fragment string) bool {
    if nil == err {
        return false
    }

    if true == strings.Contains(err.Error(), fragment) {
        return true
    }

    if joined, ok := err.(interface{ Unwrap() []error }); true == ok {
        for _, branch := range joined.Unwrap() {
            if true == chainContainsMessage(branch, fragment) {
                return true
            }
        }

        return false
    }

    return chainContainsMessage(errors.Unwrap(err), fragment)
}

/* slowThenFailingRefreshLocker answers the first renewal late but successfully — a degraded store whose reply crawls back — and fails every renewal after it. */
type slowThenFailingRefreshLocker struct {
    firstDelay   time.Duration
    refreshCount atomic.Int64
}

func (instance *slowThenFailingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &slowThenFailingRefreshLock{locker: instance}, nil
}

type slowThenFailingRefreshLock struct {
    locker *slowThenFailingRefreshLocker
}

func (instance *slowThenFailingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *slowThenFailingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *slowThenFailingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if 1 == instance.locker.refreshCount.Add(1) {
        time.Sleep(instance.locker.firstDelay)
        return nil
    }

    return exception.NewError("lease lost", nil, nil)
}

func TestRunExclusive_RenewalLeaseIsDatedFromTheRenewalIssueInstant(t *testing.T) {
    /* the first renewal is issued at 200ms and, ignoring its context, answers at 500ms; dated from the ISSUE, the lease it wrote lapses at 600ms and its demotion instant is 500ms, already come, so the caller is demoted as it lands and no second call is made. Dated from the answer, the believed lease would run to 900ms and further renewals would be attempted first. */
    locker := &slowThenFailingRefreshLocker{firstDelay: 300 * time.Millisecond}

    _, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "job:slow-renewal",
        ExclusiveOptions{Ttl: bound.Of(400 * time.Millisecond)},
        func(childRuntime containercontract.Runtime) error {
            select {
            case <-childRuntime.Context().Done():
                return nil
            case <-time.After(5 * time.Second):
                return exception.NewError("callback was never cancelled", nil, nil)
            }
        },
    )

    if nil == runErr {
        t.Fatalf("expected the lost lease to surface as an error")
    }

    if 1 != locker.refreshCount.Load() {
        t.Fatalf("expected a slow renewal dated from its issue instant to demote as it lands, got %d renewals", locker.refreshCount.Load())
    }
}

type nilableExclusiveScope struct {
    containercontract.Scope
}

type typedNilScopeExclusiveRuntime struct {
    ctx context.Context
}

func (instance *typedNilScopeExclusiveRuntime) Context() context.Context { return instance.ctx }
func (instance *typedNilScopeExclusiveRuntime) Scope() containercontract.Scope {
    return (*nilableExclusiveScope)(nil)
}
func (instance *typedNilScopeExclusiveRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

/* runtime.WithContext refuses a typed-nil scope, and the run re-wraps its runtime through it three times — once inside the deferred release, where the refusal was a second panic in the unwind of the first and the lock stayed held for its ttl; refused at the entry, nothing is held */
func TestRunExclusive_RefusesATypedNilScopeBeforeAcquiring(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    testhelper.AssertPanicsWithError(t, func() {
        _, _ = RunExclusive(
            &typedNilScopeExclusiveRuntime{ctx: context.Background()},
            locker,
            "typed-nil-scope",
            ExclusiveOptions{Ttl: bound.Of(time.Minute)},
            func(containercontract.Runtime) error { return nil },
        )
    }, "run exclusive runtime scope is nil")

    acquired, acquireErr := mustCreateLock(locker.CreateLock("typed-nil-scope", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)})).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected no lock to be held by the refused run, got acquired=%v err=%v", acquired, acquireErr)
    }
}

/* refreshHoldingLock answers each Refresh slowly, as a store on the wire does, and records whether a Release arrived while one was still in flight */
type refreshHoldingLock struct {
    inner lockcontract.Lock

    refreshInFlight             atomic.Int32
    releaseWhileRefreshInFlight atomic.Bool
    releases                    atomic.Int32
}

func (instance *refreshHoldingLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *refreshHoldingLock) Release(runtimeInstance containercontract.Runtime) error {
    if 0 < instance.refreshInFlight.Load() {
        instance.releaseWhileRefreshInFlight.Store(true)
    }

    instance.releases.Add(1)

    return instance.inner.Release(runtimeInstance)
}

func (instance *refreshHoldingLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    instance.refreshInFlight.Add(1)
    defer instance.refreshInFlight.Add(-1)

    time.Sleep(50 * time.Millisecond)

    return instance.inner.Refresh(runtimeInstance, options)
}

type refreshHoldingLocker struct {
    inner lockcontract.Locker
    last  *refreshHoldingLock
}

func (instance *refreshHoldingLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    instance.last = &refreshHoldingLock{inner: mustCreateLock(instance.inner.CreateLock(name, options))}

    return instance.last, nil
}

func TestRunExclusive_ACallbackPanicJoinsTheRefreshBeforeReleasing(t *testing.T) {
    locker := &refreshHoldingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    runtimeInstance := testRuntimeWithContext(context.Background())

    var recovered any
    func() {
        defer func() { recovered = recover() }()

        /* the callback panics while a renewal is on the wire, so the order of the join and the release is observable every time rather than by timing; the ttl leaves the 50ms renewal a quarter of it, 250ms, between the renewal at half the ttl and the demotion instant, where a few milliseconds let a late-scheduled renewal goroutine be demoted before it ever went out */
        _, _ = RunExclusive(runtimeInstance, locker, "callback-panics", ExclusiveOptions{Ttl: bound.Of(time.Second)}, func(containercontract.Runtime) error {
            deadline := time.Now().Add(5 * time.Second)
            for 0 == locker.last.refreshInFlight.Load() {
                if time.Now().After(deadline) {
                    t.Fatalf("no renewal went out while the callback ran")
                }

                time.Sleep(time.Millisecond)
            }

            panic("callback exploded")
        })
    }()

    if "callback exploded" != recovered {
        t.Fatalf("expected the callback's panic to propagate unchanged, got %v", recovered)
    }

    if true == locker.last.releaseWhileRefreshInFlight.Load() {
        t.Fatalf("expected the release to wait for the refresh in flight")
    }

    if 1 != locker.last.releases.Load() {
        t.Fatalf("expected exactly one release, got %d", locker.last.releases.Load())
    }

    acquired, acquireErr := mustCreateLock(locker.inner.CreateLock("callback-panics", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)})).Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to be released after the panic, got acquired=%v err=%v", acquired, acquireErr)
    }
}

/* hangingUntilCancelledRefreshLocker answers the acquire and never answers a renewal until the renewal's context ends, the shape of a store that accepted the call and went silent */
type hangingUntilCancelledRefreshLocker struct{}

func (instance *hangingUntilCancelledRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &hangingUntilCancelledRefreshLock{}, nil
}

type hangingUntilCancelledRefreshLock struct{}

func (instance *hangingUntilCancelledRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *hangingUntilCancelledRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *hangingUntilCancelledRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    <-runtimeInstance.Context().Done()

    return runtimeInstance.Context().Err()
}

/* measures how long after the acquire the callback is told to stop */
func cancellationDelayUnder(t *testing.T, locker lockcontract.Locker, ttl time.Duration) time.Duration {
    t.Helper()

    startedAt := time.Now()
    cancelledAfter := time.Duration(0)

    _, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:demotion-instant",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(runtimeInstance containercontract.Runtime) error {
            select {
            case <-runtimeInstance.Context().Done():
                cancelledAfter = time.Since(startedAt)
            case <-time.After(5 * time.Second):
            }

            return nil
        },
    )

    if nil == runErr {
        t.Fatalf("expected the lost lease to surface as an error")
    }

    if 0 == cancelledAfter {
        t.Fatalf("expected the callback to be cancelled")
    }

    return cancelledAfter
}

/* the renewal issued at half the ttl never answers; bounded to the demotion instant, it is cut there, and the callback is told to stop a quarter of the ttl before a contender can acquire rather than at the lapse */
func TestRunExclusive_AHungRenewalCancelsTheCallbackBeforeTheLeaseLapses(t *testing.T) {
    ttl := 400 * time.Millisecond

    if cancelledAfter := cancellationDelayUnder(t, &hangingUntilCancelledRefreshLocker{}, ttl); cancelledAfter > ttl-ttl/8 {
        t.Fatalf("expected the callback cancelled at least an eighth of the ttl before the lapse, cancelled after %v", cancelledAfter)
    }
}

/* every renewal fails at once; the one retry comes halfway to the demotion instant and fails too, and the timer demotes there instead of the next tick demoting at the lapse */
func TestRunExclusive_AFailingRenewalCancelsTheCallbackBeforeTheLeaseLapses(t *testing.T) {
    ttl := 400 * time.Millisecond

    if cancelledAfter := cancellationDelayUnder(t, &refreshFailingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}, ttl); cancelledAfter > ttl-ttl/8 {
        t.Fatalf("expected the callback cancelled at least an eighth of the ttl before the lapse, cancelled after %v", cancelledAfter)
    }
}

/* contextIgnoringRefreshLocker answers every renewal late and successfully, whatever the context it is handed says: the shape of a store client that does not honour cancellation */
type contextIgnoringRefreshLocker struct {
    inner lockcontract.Locker
    delay time.Duration
}

func (instance *contextIgnoringRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &contextIgnoringRefreshLock{inner: mustCreateLock(instance.inner.CreateLock(name, options)), delay: instance.delay}, nil
}

type contextIgnoringRefreshLock struct {
    inner lockcontract.Lock
    delay time.Duration
}

func (instance *contextIgnoringRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *contextIgnoringRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(testRuntimeWithContext(context.Background()))
}

func (instance *contextIgnoringRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    time.Sleep(instance.delay)

    return instance.inner.Refresh(testRuntimeWithContext(context.Background()), options)
}

/* the renewal issued at half the ttl ignores its context and answers success only after the demotion instant: the callback is still told to stop at the instant, and the late success does not save the lease */
func TestRunExclusive_ARenewalIgnoringItsContextCancelsTheCallbackAtTheDemotionInstant(t *testing.T) {
    ttl := 400 * time.Millisecond
    locker := &contextIgnoringRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{}), delay: 180 * time.Millisecond}

    if cancelledAfter := cancellationDelayUnder(t, locker, ttl); cancelledAfter > ttl-ttl/8 {
        t.Fatalf("expected the callback cancelled at least an eighth of the ttl before the lapse, cancelled after %v", cancelledAfter)
    }
}

/* the callback returns before the demotion instant while a renewal ignoring its context is still on the wire and answers after it: the run ended with its lease, so it is reported clean rather than as a lost lease. The renewal goes out at 1s and answers at 1.9s, past the 1.5s instant; the callback returns at 1.25s, a quarter of a second from each, where a 50ms margin before the instant lost the race to a loaded runner */
func TestRunExclusive_ACallbackThatReturnedBeforeTheInstantIsACleanRunWhateverTheRenewalAnswersLate(t *testing.T) {
    ttl := 2 * time.Second
    locker := &contextIgnoringRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{}), delay: 900 * time.Millisecond}

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:returned-before-the-instant",
        ExclusiveOptions{Ttl: bound.Of(ttl)},
        func(containercontract.Runtime) error {
            time.Sleep(1250 * time.Millisecond)

            return nil
        },
    )

    if false == ran || nil != runErr {
        t.Fatalf("expected a clean run, got ran=%v err=%v", ran, runErr)
    }
}

func TestRunExclusive_TheZeroConfigurationHandsTheLockerAThirtySecondLease(t *testing.T) {
    locker := &ttlRecordingLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    ran, runErr := RunExclusive(testRuntime(), locker, "zero-config", ExclusiveOptions{}, func(containercontract.Runtime) error {
        return nil
    })
    if false == ran || nil != runErr {
        t.Fatalf("expected the zero configuration to run, got %v %v", ran, runErr)
    }

    if ttl, isSet := locker.firstTtl(t).Value(); false == isSet || 30*time.Second != ttl {
        t.Fatalf("expected the locker to be handed a 30s lease, got %s", locker.firstTtl(t))
    }
}

func TestRunExclusive_AnUnlimitedTtlHandsTheLockerASessionLock(t *testing.T) {
    locker := &ttlRecordingLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}}

    ran, runErr := RunExclusive(testRuntime(), locker, "session-config", ExclusiveOptions{Ttl: bound.UnlimitedDuration()}, func(containercontract.Runtime) error {
        return nil
    })
    if false == ran || nil != runErr {
        t.Fatalf("expected the session-style run to run, got %v %v", ran, runErr)
    }

    if false == locker.firstTtl(t).IsUnlimited() {
        t.Fatalf("expected the locker to be handed an unlimited ttl, got %s", locker.firstTtl(t))
    }
}

/* releaseDeadlineLocker hands out locks that acquire at once and record how long the release they are handed has left. */
type releaseDeadlineLocker struct {
    remaining  chan time.Duration
    releaseErr chan error
}

func (instance *releaseDeadlineLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &releaseDeadlineLock{remaining: instance.remaining, releaseErr: instance.releaseErr}, nil
}

type releaseDeadlineLock struct {
    remaining  chan time.Duration
    releaseErr chan error
}

func (instance *releaseDeadlineLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *releaseDeadlineLock) Release(runtimeInstance containercontract.Runtime) error {
    if nil != instance.releaseErr {
        instance.releaseErr <- runtimeInstance.Context().Err()
    }

    deadline, hasDeadline := runtimeInstance.Context().Deadline()
    if false == hasDeadline {
        instance.remaining <- -1

        return nil
    }

    instance.remaining <- time.Until(deadline)

    return nil
}

func (instance *releaseDeadlineLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

func TestRunExclusive_ReleasesOnAContextDetachedFromTheRunWithNoDeadlineOfItsOwn(t *testing.T) {
    locker := &releaseDeadlineLocker{remaining: make(chan time.Duration, 1), releaseErr: make(chan error, 1)}

    runContext, cancelRun := context.WithCancel(context.Background())
    runtimeInstance := testRuntimeUnder(runContext)

    _, runErr := RunExclusive(runtimeInstance, locker, "release-detached", ExclusiveOptions{}, func(containercontract.Runtime) error {
        cancelRun()

        return nil
    })
    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if remaining := <-locker.remaining; -1 != remaining {
        t.Fatalf("expected the release handed no deadline of the helper's, got %s left", remaining)
    }

    if contextErr := <-locker.releaseErr; nil != contextErr {
        t.Fatalf("expected the release context alive after the run was cancelled, got %v", contextErr)
    }
}

func TestRunExclusive_RenewsTheLeaseWhileTheCallbackWindsDownAfterAStop(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    inner := NewInMemoryLocker(InMemoryLockerConfig{})
    locker := &countingRefreshLocker{inner: inner}

    var atStop int64
    ran, err := RunExclusive(testRuntimeWithContext(parentContext), locker, "job:wind-down", ExclusiveOptions{Ttl: bound.Of(40 * time.Millisecond)}, func(childRuntime containercontract.Runtime) error {
        cancelParent()
        <-childRuntime.Context().Done()
        atStop = locker.refreshes()

        time.Sleep(120 * time.Millisecond)

        return nil
    })
    if false == ran || nil != err {
        t.Fatalf("expected a clean run, got %v %v", ran, err)
    }

    if renewed := locker.refreshes() - atStop; renewed < 2 {
        t.Fatalf("expected the lease renewed while the callback wound down, got %d renewals after the stop", renewed)
    }

    if acquired, _ := mustCreateLock(inner.CreateLock("job:wind-down", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Second)})).Acquire(testRuntimeWithContext(context.Background())); false == acquired {
        t.Fatal("expected the lock released once the callback returned")
    }
}

func TestRunExclusive_StopsRenewingPastTheStopRenewalLimit(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    locker := &countingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    var pastLimit int64
    _, err := RunExclusive(testRuntimeWithContext(parentContext), locker, "job:past-limit", ExclusiveOptions{Ttl: bound.Of(40 * time.Millisecond), StopRenewalLimit: bound.Of(60 * time.Millisecond)}, func(childRuntime containercontract.Runtime) error {
        cancelParent()

        time.Sleep(100 * time.Millisecond)
        pastLimit = locker.refreshes()
        time.Sleep(100 * time.Millisecond)

        return nil
    })

    if locker.refreshes() != pastLimit {
        t.Fatalf("expected no renewal past the limit, got %d after it", locker.refreshes()-pastLimit)
    }

    if nil == err || false == strings.Contains(err.Error(), "lost the lock lease") || false == strings.Contains(err.Error()+errorChainText(err), "stop renewal limit") {
        t.Fatalf("expected the run to name the stop renewal limit, got %v", err)
    }
}

func TestRunExclusive_ALeaseLostDuringTheWindDownIsReported(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    locker := &switchableRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    _, err := RunExclusive(testRuntimeWithContext(parentContext), locker, "job:lost-in-wind-down", ExclusiveOptions{Ttl: bound.Of(40 * time.Millisecond)}, func(childRuntime containercontract.Runtime) error {
        locker.fail.Store(true)
        cancelParent()

        time.Sleep(150 * time.Millisecond)

        return nil
    })

    if nil == err || false == strings.Contains(err.Error(), "lost the lock lease") {
        t.Fatalf("expected the lease lost during the wind-down reported, got %v", err)
    }
}

func TestRunExclusive_RefusesAStopRenewalLimitBelowTheRefreshInterval(t *testing.T) {
    _, err := RunExclusive(testRuntimeWithContext(context.Background()), NewInMemoryLocker(InMemoryLockerConfig{}), "job:limit", ExclusiveOptions{Ttl: bound.Of(time.Second), StopRenewalLimit: bound.Of(100 * time.Millisecond)}, func(containercontract.Runtime) error {
        t.Fatal("expected the callback not run")

        return nil
    })

    exceptionError, isExceptionError := err.(*exception.Error)
    if false == isExceptionError || "ExclusiveOptions.StopRenewalLimit" != exceptionError.Context()["field"] {
        t.Fatalf("expected the limit refused naming its field, got %v", err)
    }
}

func errorChainText(err error) string {
    text := ""
    for cause := errors.Unwrap(err); nil != cause; cause = errors.Unwrap(cause) {
        text += " | " + cause.Error()
    }

    if joined, isJoined := errors.Unwrap(err).(interface{ Unwrap() []error }); true == isJoined {
        for _, member := range joined.Unwrap() {
            text += " | " + member.Error()
        }
    }

    return text
}

/* the join cancels the renewal's own context, which the stop does not reach, so a refresh blocked when callback returns is interrupted at once instead of waiting out its budget with the lock held */
func TestRunExclusive_TheJoinInterruptsARefreshBlockedAtReturn(t *testing.T) {
    entered := make(chan struct{})
    locker := &blockingRefreshLocker{entered: entered}

    var returnedAt time.Time
    _, runErr := RunExclusive(testRuntimeWithContext(context.Background()), locker, "blocking-join", ExclusiveOptions{Ttl: bound.Of(4 * time.Second)}, func(childRuntime containercontract.Runtime) error {
        waitErr := waitForRefreshInFlight(childRuntime, entered)
        returnedAt = time.Now()

        return waitErr
    })
    if nil != runErr {
        t.Fatalf("expected a clean run, got %v", runErr)
    }

    if waited := time.Since(returnedAt); waited > 500*time.Millisecond {
        t.Fatalf("expected the join to interrupt the blocked refresh at once, waited %v", waited)
    }
}

func TestRunExclusive_TheStopRenewalLimitRunsFromTheStopAndWarnsAtOnce(t *testing.T) {
    parentContext, cancelParent := context.WithCancel(context.Background())
    defer cancelParent()

    runtimeInstance, logger := runtimeWithRecordingLogger(parentContext)
    locker := &countingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    var atStop, beforeLimit int64
    warnedBeforeReturn := false
    _, err := RunExclusive(runtimeInstance, locker, "job:limit-from-stop", ExclusiveOptions{Ttl: bound.Of(40 * time.Millisecond), StopRenewalLimit: bound.Of(80 * time.Millisecond)}, func(childRuntime containercontract.Runtime) error {
        time.Sleep(120 * time.Millisecond)
        cancelParent()
        atStop = locker.refreshes()

        time.Sleep(50 * time.Millisecond)
        beforeLimit = locker.refreshes()

        time.Sleep(150 * time.Millisecond)
        warnedBeforeReturn = logger.hasMessageContaining("stop renewal limit")

        return nil
    })

    if beforeLimit <= atStop {
        t.Fatalf("expected renewals in the limit counted from the stop, got %d at the stop and %d before the limit", atStop, beforeLimit)
    }

    if false == warnedBeforeReturn {
        t.Fatal("expected the warning written when the limit passed, before the callback returned")
    }

    if nil == err {
        t.Fatal("expected the run to answer the limit")
    }
}

func TestRunExclusive_ALockersRefusalIsTheRunsErrorAndTheCallbackDoesNotRun(t *testing.T) {
    called := false

    ran, runErr := RunExclusive(testRuntime(), NewInMemoryLocker(InMemoryLockerConfig{}), "job:unlimited", ExclusiveOptions{Ttl: bound.UnlimitedDuration()}, func(runtimeInstance containercontract.Runtime) error {
        called = true

        return nil
    })

    if true == ran || true == called || false == errors.Is(runErr, lockcontract.ErrUnlimitedTtlRefused) {
        t.Fatalf("expected the refusal and no run, got %v %v %v", ran, called, runErr)
    }
}

func TestRunExclusive_ALockerAnsweringNoLockIsTheRunsErrorAndTheCallbackDoesNotRun(t *testing.T) {
    called := false

    ran, runErr := RunExclusive(testRuntime(), &noLockLocker{}, "job:none", ExclusiveOptions{Ttl: bound.Of(time.Second)}, func(runtimeInstance containercontract.Runtime) error {
        called = true

        return nil
    })

    if true == ran || true == called || false == namesNoLock(runErr) {
        t.Fatalf("expected the no-lock refusal and no run, got %v %v %v", ran, called, runErr)
    }
}

func TestResolveSessionProbeInterval_DefaultsMinimumUnlimitedAndALeaseTtl(t *testing.T) {
    unlimitedTtl := bound.Resolved[time.Duration]{Unlimited: true}

    if probeInterval, probeErr := resolveSessionProbeInterval(exclusiveProbeIntervalField, bound.Duration{}, unlimitedTtl); nil != probeErr || 15*time.Second != probeInterval {
        t.Fatalf("expected a left-out probe interval to resolve to 15s, got %s %v", probeInterval, probeErr)
    }

    if probeInterval, probeErr := resolveSessionProbeInterval(exclusiveProbeIntervalField, bound.Of(time.Millisecond), unlimitedTtl); nil != probeErr || time.Millisecond != probeInterval {
        t.Fatalf("expected the 1ms minimum to be admitted, got %s %v", probeInterval, probeErr)
    }

    if probeInterval, probeErr := resolveSessionProbeInterval(exclusiveProbeIntervalField, bound.Duration{}, bound.Resolved[time.Duration]{Value: 30 * time.Second}); nil != probeErr || 15*time.Second != probeInterval {
        t.Fatalf("expected a left-out probe interval beside a lease ttl to resolve quietly, got %s %v", probeInterval, probeErr)
    }

    for _, testCase := range []struct {
        name       string
        configured bound.Duration
        ttl        bound.Resolved[time.Duration]
        message    string
    }{
        {"below the minimum", bound.Of(time.Microsecond), unlimitedTtl, "the limit is below its minimum"},
        {"unlimited", bound.UnlimitedDuration(), unlimitedTtl, "the limit does not admit unlimited"},
        {"beside a lease ttl", bound.Of(time.Second), bound.Resolved[time.Duration]{Value: 30 * time.Second}, "the probe interval applies to an unlimited ttl only"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            _, probeErr := resolveSessionProbeInterval(exclusiveProbeIntervalField, testCase.configured, testCase.ttl)
            if nil == probeErr || testCase.message != probeErr.Error() {
                t.Fatalf("expected %q, got %v", testCase.message, probeErr)
            }

            if exclusiveProbeIntervalField != exception.LogContext(probeErr, exception.LogContextOptions{})["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", exclusiveProbeIntervalField, exception.LogContext(probeErr, exception.LogContextOptions{}))
            }
        })
    }
}

func TestRunExclusive_ASessionLockIsProbedAtTheConfiguredInterval(t *testing.T) {
    locker := &refreshRecordingLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}}

    ran, runErr := RunExclusive(testRuntime(), locker, "session-probe", ExclusiveOptions{Ttl: bound.UnlimitedDuration(), ProbeInterval: bound.Of(5 * time.Millisecond)}, func(containercontract.Runtime) error {
        time.Sleep(60 * time.Millisecond)

        return nil
    })
    if false == ran || nil != runErr {
        t.Fatalf("expected the session-style run to run, got %v %v", ran, runErr)
    }

    refreshTtls := locker.refreshTtls()
    if 2 > len(refreshTtls) {
        t.Fatalf("expected a 5ms probe to refresh at least twice in 60ms, got %d", len(refreshTtls))
    }

    if wanted := bound.Of(sessionProbeTtlFactor * 5 * time.Millisecond); wanted != refreshTtls[0] {
        t.Fatalf("expected each probe to write twice its interval, got %s", refreshTtls[0])
    }
}

func TestRunExclusive_RefusesAProbeIntervalBesideALeaseTtlNamingTheField(t *testing.T) {
    ran, runErr := RunExclusive(testRuntime(), NewInMemoryLocker(InMemoryLockerConfig{}), "probe-lease", ExclusiveOptions{ProbeInterval: bound.Of(time.Second)}, func(containercontract.Runtime) error {
        t.Fatalf("expected the callback not to run")

        return nil
    })
    if true == ran || nil == runErr || "ExclusiveOptions.ProbeInterval" != exception.LogContext(runErr, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected the refusal naming %s, got %v %v", exclusiveProbeIntervalField, ran, runErr)
    }
}
