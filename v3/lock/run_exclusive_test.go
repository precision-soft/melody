package lock

import (
    "context"
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
        t.Fatalf("expected the fn error to propagate, got: %v", runErr)
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
    inner        lockcontract.Locker
    refreshTtl   chan time.Duration
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

/** @info A lease-style backend rewrites the lease to now+ttl on Refresh. Handing it the probe interval itself would renew the lease exactly as it expires, so every probe races its own expiry: fn is cancelled spuriously and the lapsed lease lets a second instance run alongside it. The probe must renew for a multiple of its own cadence, and the derived interval must never reach time.NewTicker as zero. */
func TestResolveRefreshSchedule(t *testing.T) {
    for _, testCase := range []struct {
        name            string
        ttl             time.Duration
        wantedInterval  time.Duration
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

/** @info A ttl of a few nanoseconds makes ttl/2 == 0 and time.NewTicker(0) panics on the refresh goroutine — a panic no recover can reach. The derived interval must be floored. */
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
        t.Fatal("expected fn to run")
    }
    _ = runErr
}

/** @info A Refresh already blocked on an unresponsive backend when fn returns must be interrupted: closing refreshDone alone cannot reach it, so RunExclusive would wait on the goroutine forever while holding the lock. Cancelling the child context before Wait unblocks it, and the resulting error must read as shutdown, not as a lost lease. */
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
        t.Fatal("RunExclusive hung with a refresh in flight at fn return")
    }
}
