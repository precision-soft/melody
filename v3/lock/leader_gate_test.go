package lock

import (
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/exception"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type switchableRefreshLocker struct {
    inner lockcontract.Locker
    fail  atomic.Bool
}

func (instance *switchableRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &switchableRefreshLock{
        locker: instance,
        inner:  instance.inner.CreateLock(name, ttl),
    }
}

type switchableRefreshLock struct {
    locker *switchableRefreshLocker
    inner  lockcontract.Lock
}

func (instance *switchableRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *switchableRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *switchableRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if true == instance.locker.fail.Load() {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

func fastGateOptions() LeaderGateOptions {
    return LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
    }
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool, message string) {
    t.Helper()

    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if true == condition() {
            return
        }

        time.Sleep(2 * time.Millisecond)
    }

    t.Fatalf("condition not reached within %v: %s", timeout, message)
}

func TestLeaderGate_ExactlyOneLeader(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    firstContext, firstCancel := context.WithCancel(context.Background())
    defer firstCancel()
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGateWithOptions(locker, "worker:leader", time.Minute, fastGateOptions())
    second := NewLeaderGateWithOptions(locker, "worker:leader", time.Minute, fastGateOptions())

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)
    go func() {
        defer waitGroup.Done()
        _ = first.Run(testRuntimeWithContext(firstContext))
    }()
    go func() {
        defer waitGroup.Done()
        _ = second.Run(testRuntimeWithContext(secondContext))
    }()

    waitUntil(t, 2*time.Second, func() bool {
        return first.IsLeader() != second.IsLeader()
    }, "expected exactly one of the two gates to lead")

    /* hold the state for a few refresh cycles: still exactly one leader */
    time.Sleep(30 * time.Millisecond)
    if first.IsLeader() == second.IsLeader() {
        t.Fatalf("expected exactly one leader to persist: first=%v second=%v", first.IsLeader(), second.IsLeader())
    }

    firstCancel()
    secondCancel()
    waitGroup.Wait()
}

func TestLeaderGate_FailoverOnLeaderShutdown(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    firstContext, firstCancel := context.WithCancel(context.Background())
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGateWithOptions(locker, "worker:failover", time.Minute, fastGateOptions())
    second := NewLeaderGateWithOptions(locker, "worker:failover", time.Minute, fastGateOptions())

    firstDone := make(chan error, 1)
    go func() {
        firstDone <- first.Run(testRuntimeWithContext(firstContext))
    }()

    waitUntil(t, 2*time.Second, first.IsLeader, "expected the first gate to lead")

    secondDone := make(chan error, 1)
    go func() {
        secondDone <- second.Run(testRuntimeWithContext(secondContext))
    }()

    /* the second gate keeps campaigning while the first leads */
    time.Sleep(20 * time.Millisecond)
    if true == second.IsLeader() {
        t.Fatalf("expected the second gate to wait while the first leads")
    }

    firstCancel()
    if runErr := <-firstDone; nil != runErr {
        t.Fatalf("expected a clean shutdown from the first gate, got: %v", runErr)
    }

    /* the shutdown released the lock, so failover must not wait out any ttl */
    waitUntil(t, 2*time.Second, second.IsLeader, "expected the second gate to take over after shutdown")

    secondCancel()
    <-secondDone
}

func TestLeaderGate_DemotesAndReelectsOnRefreshFailure(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())

    /* fail refreshes only while enabled, so the gate can lose the lease once and then win it back */
    failing := &switchableRefreshLocker{inner: innerLocker}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCauses []error
    var lostMutex sync.Mutex
    elected := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:lease", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lostMutex.Lock()
            defer lostMutex.Unlock()
            lostCauses = append(lostCauses, cause)
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    <-elected

    waitUntil(t, 2*time.Second, func() bool {
        lostMutex.Lock()
        defer lostMutex.Unlock()
        return 0 < len(lostCauses)
    }, "expected the gate to lose leadership on refresh failure")

    lostMutex.Lock()
    if nil == lostCauses[0] {
        lostMutex.Unlock()
        t.Fatalf("expected the lost cause to carry the refresh error")
    }
    lostMutex.Unlock()

    /* let refreshes succeed again: the gate must campaign back to leadership */
    failing.fail.Store(false)
    <-elected

    waitUntil(t, 2*time.Second, gate.IsLeader, "expected re-election once refreshes succeed")

    cancel()
    <-done
}

func TestLeaderGate_ReleasesOnShutdown(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())

    gate := NewLeaderGateWithOptions(locker, "worker:release", time.Hour, fastGateOptions())

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    waitUntil(t, 2*time.Second, gate.IsLeader, "expected the gate to lead")

    cancel()
    if runErr := <-done; nil != runErr {
        t.Fatalf("expected a clean shutdown, got: %v", runErr)
    }

    if true == gate.IsLeader() {
        t.Fatalf("expected the gate to drop leadership on shutdown")
    }

    /* the one-hour ttl must not matter: shutdown releases the lock immediately */
    contender := locker.CreateLock("worker:release", time.Minute)
    acquired, _ := contender.Acquire(testRuntime())
    if false == acquired {
        t.Fatalf("expected the lock to be free right after shutdown")
    }
}
