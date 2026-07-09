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
