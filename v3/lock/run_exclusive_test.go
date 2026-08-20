package lock

import (
    "context"
    "errors"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRuntimeWithContext(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

type acquireFailingLocker struct{}

func (instance *acquireFailingLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &acquireFailingLock{}
}

type acquireFailingLock struct{}

func (instance *acquireFailingLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return false, exception.NewError("store unreachable", nil, nil)
}

func (instance *acquireFailingLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *acquireFailingLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return nil
}

type refreshFailingLocker struct {
    inner lockcontract.Locker
}

func (instance *refreshFailingLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &refreshFailingLock{inner: instance.inner.CreateLock(name, ttl)}
}

type refreshFailingLock struct {
    inner lockcontract.Lock
}

func (instance *refreshFailingLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *refreshFailingLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *refreshFailingLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return exception.NewError("lease lost", nil, nil)
}

/* contextCancelledAcquireLock stands in for a backend round trip cut short by a SIGTERM: Acquire fails
with the runtime cancellation wrapped in a store error, exactly as a real locker reports an in-flight
Acquire when the context it was called with is cancelled underneath it. */
type contextCancelledAcquireLocker struct{}

func (instance *contextCancelledAcquireLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &contextCancelledAcquireLock{}
}

type contextCancelledAcquireLock struct{}

func (instance *contextCancelledAcquireLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return false, exception.NewError("acquire aborted", nil, runtimeInstance.Context().Err())
}

func (instance *contextCancelledAcquireLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *contextCancelledAcquireLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return nil
}

func TestRunExclusive_RunsAndReleases(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    ranFirst := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:tick", time.Minute, func(childRuntime runtimecontract.Runtime) error {
        ranFirst = true
        return nil
    })
    if nil != runErr || false == ran || false == ranFirst {
        t.Fatalf("expected first run to execute: ran=%v err=%v", ran, runErr)
    }

    /* the lock must be released right after the run, not after the ttl */
    ranSecond := false
    ran, runErr = RunExclusive(runtimeInstance, locker, "cron:tick", time.Minute, func(childRuntime runtimecontract.Runtime) error {
        ranSecond = true
        return nil
    })
    if nil != runErr || false == ran || false == ranSecond {
        t.Fatalf("expected second run to execute after release: ran=%v err=%v", ran, runErr)
    }
}

func TestRunExclusive_SkipsWhenHeldByAnotherHolder(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    holder := locker.CreateLock("cron:held", time.Minute)
    acquired, _ := holder.Acquire(runtimeInstance)
    if false == acquired {
        t.Fatalf("expected external holder to acquire")
    }

    called := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:held", time.Minute, func(childRuntime runtimecontract.Runtime) error {
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
    ran, runErr := RunExclusive(runtimeInstance, &acquireFailingLocker{}, "cron:error", time.Minute, func(childRuntime runtimecontract.Runtime) error {
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
    /* a SIGTERM cancels the very context the backend was called with, so an in-flight Acquire fails with the cancellation wrapped in a store error; that is the stop itself and must read as a clean skip, never as an error a cron fleet reports as a failed run */
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := testRuntimeWithContext(cancelledContext)

    called := false
    ran, runErr := RunExclusive(runtimeInstance, &contextCancelledAcquireLocker{}, "cron:sigterm", time.Minute, func(childRuntime runtimecontract.Runtime) error {
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
    locker := NewInMemoryLocker(clock.NewSystemClock())
    runtimeInstance := testRuntime()

    expected := exception.NewError("work failed", nil, nil)
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:failing", time.Minute, func(childRuntime runtimecontract.Runtime) error {
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
    locker := &refreshFailingLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    runtimeInstance := testRuntime()

    cancelled := false
    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:lease", 20*time.Millisecond, func(childRuntime runtimecontract.Runtime) error {
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
    locker := NewInMemoryLocker(clock.NewSystemClock())

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := testRuntimeWithContext(cancelledContext)

    ran, runErr := RunExclusive(runtimeInstance, locker, "cron:sigterm", time.Minute, func(childRuntime runtimecontract.Runtime) error {
        return nil
    })
    if nil != runErr || false == ran {
        t.Fatalf("expected the run to execute: ran=%v err=%v", ran, runErr)
    }

    /* the release must have gone through on the detached runtime despite the cancelled caller context */
    contender := locker.CreateLock("cron:sigterm", time.Minute)
    acquired, _ := contender.Acquire(testRuntime())
    if false == acquired {
        t.Fatalf("expected the lock to be released despite the cancelled caller context")
    }
}

/* recordingRefreshLocker captures the ttl each Refresh is asked to write, so a test can assert the lease
margin the refresher gives a lease-style backend. */
type recordingRefreshLocker struct {
    inner      lockcontract.Locker
    refreshTtl chan time.Duration
}

func (instance *recordingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &recordingRefreshLock{inner: instance.inner.CreateLock(name, ttl), refreshTtl: instance.refreshTtl}
}

type recordingRefreshLock struct {
    inner      lockcontract.Lock
    refreshTtl chan time.Duration
}

func (instance *recordingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *recordingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *recordingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    select {
    case instance.refreshTtl <- ttl:
    default:
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* blockingRefreshLock blocks inside Refresh until its runtime context is cancelled, standing in for a
backend whose connection has been blackholed by a network partition. */
type blockingRefreshLocker struct {
    entered chan struct{}
}

func (instance *blockingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &blockingRefreshLock{entered: instance.entered}
}

type blockingRefreshLock struct {
    entered chan struct{}
    closed  bool
}

func (instance *blockingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *blockingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *blockingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if false == instance.closed {
        instance.closed = true
        close(instance.entered)
    }

    <-runtimeInstance.Context().Done()

    return exception.NewError("refresh aborted", nil, runtimeInstance.Context().Err())
}

/* @info A lease-style backend rewrites the lease to now+ttl on Refresh. Handing it the probe interval itself would renew the lease exactly as it expires, so every probe races its own expiry: callback is cancelled spuriously and the lapsed lease lets a second instance run alongside it. The probe must renew for a multiple of its own cadence, and the derived interval must never reach time.NewTicker as zero. */
func TestResolveRefreshSchedule(t *testing.T) {
    for _, testCase := range []struct {
        name             string
        ttl              time.Duration
        wantedInterval   time.Duration
        wantedRefreshTtl time.Duration
    }{
        {"positive ttl refreshes at half of it", 30 * time.Second, 15 * time.Second, 30 * time.Second},
        {"zero ttl probes with a lease margin", 0, defaultSessionProbeInterval, sessionProbeTtlFactor * defaultSessionProbeInterval},
        {"negative ttl probes with a lease margin", -1 * time.Second, defaultSessionProbeInterval, sessionProbeTtlFactor * defaultSessionProbeInterval},
        {"tiny ttl is floored off zero", 1 * time.Nanosecond, minimumRefreshInterval, 1 * time.Nanosecond},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            interval, refreshTtl := resolveRefreshSchedule(testCase.ttl)

            if testCase.wantedInterval != interval {
                t.Fatalf("interval: wanted %s, got %s", testCase.wantedInterval, interval)
            }
            if testCase.wantedRefreshTtl != refreshTtl {
                t.Fatalf("refresh ttl: wanted %s, got %s", testCase.wantedRefreshTtl, refreshTtl)
            }
            if 0 >= interval {
                t.Fatal("a non-positive interval would panic time.NewTicker")
            }

            /* a ttl below the floor is a misconfiguration the lock cannot rescue — the lease expires before
               any renewal can land — but it must not panic; every sane ttl renews with slack to spare */
            if minimumRefreshInterval <= testCase.ttl || 0 >= testCase.ttl {
                if 0 >= refreshTtl-interval {
                    t.Fatalf("the refresh ttl %s gives no margin over the %s cadence", refreshTtl, interval)
                }
            }
        })
    }
}

/* @info A ttl of a few nanoseconds makes ttl/2 == 0 and time.NewTicker(0) panics on the refresh goroutine — a panic no recover can reach. The derived interval must be floored. */
func TestRunExclusive_TinyTtlDoesNotPanicTheRefreshGoroutine(t *testing.T) {
    ran := false

    acquired, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        NewInMemoryLocker(clock.NewSystemClock()),
        "tiny-ttl",
        1*time.Nanosecond,
        func(runtimecontract.Runtime) error {
            ran = true
            time.Sleep(5 * time.Millisecond)
            return nil
        },
    )

    if false == acquired {
        t.Fatal("expected the lock to be acquired")
    }
    if false == ran {
        t.Fatal("expected callback to run")
    }
    _ = runErr
}

/* @info A Refresh already blocked on an unresponsive backend when callback returns must be interrupted: closing refreshDone alone cannot reach it, so RunExclusive would wait on the goroutine forever while holding the lock. Cancelling the child context before Wait unblocks it, and the resulting error must read as shutdown, not as a lost lease. */
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
            2*time.Millisecond,
            func(runtimecontract.Runtime) error {
                <-entered
                return nil
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

/* @info A SIGTERM cancels the runtime the refresh loop calls the backend with, so a refresh in flight fails with that cancellation. That is the shutdown itself, not a lost lease: reporting it turns every graceful stop of a long-running exclusive command into an error a cron fleet reads as a failed run. */
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
        20*time.Millisecond,
        func(childRuntime runtimecontract.Runtime) error {
            /* wait until a refresh is genuinely in flight, then shut the process down underneath it */
            <-entered
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
    locker := &hangingRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    ttl := 400 * time.Millisecond
    cancelled := false

    ran, err := RunExclusive(
        testRuntimeWithContext(runContext),
        locker,
        "job:hanging-refresh",
        ttl,
        func(childRuntime runtimecontract.Runtime) error {
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

func (instance *slowSucceedingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &slowSucceedingRefreshLock{delay: instance.delay}
}

type slowSucceedingRefreshLock struct {
    delay time.Duration
}

func (instance *slowSucceedingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *slowSucceedingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *slowSucceedingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    select {
    case <-time.After(instance.delay):
        return nil
    case <-runtimeInstance.Context().Done():
        return runtimeInstance.Context().Err()
    }
}

/* @info A renewal that ANSWERS, inside the lease it is renewing, renewed it — however long the store took to say so. Demoting on the latency of one call instead of on the lease clock cancels work that was never in danger and, under a LeaderGate, drops a term that was never lost. The delay here sits above the old per-call verdict (a quarter of the ttl) and below the lease, which is exactly the band that used to report a lost lock. */
func TestRunExclusive_ASlowButSuccessfulRenewalDoesNotLoseTheLease(t *testing.T) {
    ttl := 200 * time.Millisecond
    locker := &slowSucceedingRefreshLocker{delay: 80 * time.Millisecond}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:slow-store",
        ttl,
        func(runtimeInstance runtimecontract.Runtime) error {
            time.Sleep(500 * time.Millisecond)
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
        t.Fatal("the callback was cancelled: a renewal that succeeded inside the lease was read as a lost lock")
    }
}

/* @info the other half of the invariant: a renewal that keeps failing must still demote, and must do so before the lease it last wrote can be acquired by anyone else rather than after. */
func TestRunExclusive_APersistentlyFailingRenewalStillLosesTheLease(t *testing.T) {
    ttl := 200 * time.Millisecond
    locker := &refreshFailingLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:dead-store",
        ttl,
        func(runtimeInstance runtimecontract.Runtime) error {
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

func (instance *intermittentRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &intermittentRefreshLock{}
}

type intermittentRefreshLock struct {
    attempts atomic.Int64
}

func (instance *intermittentRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *intermittentRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *intermittentRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 1 == instance.attempts.Add(1) {
        return exception.NewError("refresh dropped by the store", nil, nil)
    }

    return nil
}

/* @info One failed renewal is not a lost lease. The renewal cadence is half the ttl precisely so that a lost attempt still leaves a whole second attempt before the lease lapses; a policy that demotes on the first failure throws that margin away and turns every dropped connection into cancelled work and, under a LeaderGate, a dropped term. What must demote is the lease clock — the attempt after the failure lands, rewrites the lease, and nothing was ever in danger. */
func TestRunExclusive_ASingleFailedRenewalIsSurvivedByTheNextOne(t *testing.T) {
    ttl := 200 * time.Millisecond
    locker := &intermittentRefreshLocker{}

    completed := false

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "worker:flaky-store",
        ttl,
        func(runtimeInstance runtimecontract.Runtime) error {
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

func (instance *slowAcquireCountingLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &slowAcquireCountingLock{locker: instance}
}

type slowAcquireCountingLock struct {
    locker *slowAcquireCountingLocker
}

func (instance *slowAcquireCountingLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    time.Sleep(instance.locker.acquireDelay)
    return true, nil
}

func (instance *slowAcquireCountingLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *slowAcquireCountingLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.locker.refreshCount.Add(1)
    return exception.NewError("lease lost", nil, nil)
}

func TestRunExclusive_LeaseIsDatedFromTheAcquireIssueInstant(t *testing.T) {
    /* the acquire answers 600ms after it was issued, against a 400ms ttl: the lease the store wrote has already lapsed by the time callback starts. Dated from the ISSUE instant, the very first failed renewal finds the lease beyond recovery and demotes; dated from the ANSWER — the defect — the believed lease ran a further 600ms past the real one, the first failure was read as survivable, and the callback kept running through a window in which a second instance could legally acquire. The refresh count at demotion is the observable that separates the two datings. */
    locker := &slowAcquireCountingLocker{acquireDelay: 600 * time.Millisecond}

    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "job:slow-acquire",
        400*time.Millisecond,
        func(childRuntime runtimecontract.Runtime) error {
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

    if 1 != locker.refreshCount.Load() {
        t.Fatalf("expected the first failed renewal to demote a lease dated from the acquire issue instant, got %d renewals", locker.refreshCount.Load())
    }
}

/* releaseFailingLocker acquires and refreshes cleanly and fails every release — the shape of a store hiccup exactly at release time. */
type releaseFailingLocker struct{}

func (instance *releaseFailingLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &releaseFailingLock{}
}

type releaseFailingLock struct{}

func (instance *releaseFailingLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *releaseFailingLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return exception.NewError("store unreachable at release", nil, nil)
}

func (instance *releaseFailingLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return nil
}

func TestRunExclusive_FailedReleaseIsLogged(t *testing.T) {
    runtimeInstance, logger := runtimeWithRecordingLogger(context.Background())

    ran, runErr := RunExclusive(
        runtimeInstance,
        &releaseFailingLocker{},
        "job:release-fails",
        time.Minute,
        func(childRuntime runtimecontract.Runtime) error {
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
        &refreshFailingLocker{inner: NewInMemoryLocker(clock.NewSystemClock())},
        "job:joined-cause",
        100*time.Millisecond,
        func(childRuntime runtimecontract.Runtime) error {
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

func (instance *panickingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &panickingRefreshLock{}
}

type panickingRefreshLock struct{}

func (instance *panickingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *panickingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *panickingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    panic(exception.NewError("backend exploded", nil, nil))
}

func TestRunExclusive_PanickingRefreshDemotesInsteadOfKillingTheProcess(t *testing.T) {
    ran, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        &panickingRefreshLocker{},
        "job:refresh-panics",
        50*time.Millisecond,
        func(childRuntime runtimecontract.Runtime) error {
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

func (instance *slowThenFailingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &slowThenFailingRefreshLock{locker: instance}
}

type slowThenFailingRefreshLock struct {
    locker *slowThenFailingRefreshLocker
}

func (instance *slowThenFailingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return true, nil
}

func (instance *slowThenFailingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *slowThenFailingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 1 == instance.locker.refreshCount.Add(1) {
        time.Sleep(instance.locker.firstDelay)
        return nil
    }

    return exception.NewError("lease lost", nil, nil)
}

func TestRunExclusive_RenewalLeaseIsDatedFromTheRenewalIssueInstant(t *testing.T) {
    /* the first renewal is issued at 200ms and answers at 500ms; dated from the ISSUE, the lease it wrote lapses at 600ms, so the failure right behind it (issued at 500ms, inside the 100ms recovery margin of that lease) demotes at the SECOND call. Dated from the answer — the defect — the believed lease ran to 900ms and two more failures were read as survivable first. */
    locker := &slowThenFailingRefreshLocker{firstDelay: 300 * time.Millisecond}

    _, runErr := RunExclusive(
        testRuntimeWithContext(context.Background()),
        locker,
        "job:slow-renewal",
        400*time.Millisecond,
        func(childRuntime runtimecontract.Runtime) error {
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

    if 2 != locker.refreshCount.Load() {
        t.Fatalf("expected the failure behind a slow successful renewal to demote a lease dated from the renewal issue instant, got %d renewals", locker.refreshCount.Load())
    }
}
