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

/** @info A gate that can never acquire — a redis locker built with a non-positive ttl fails closed on every Acquire — is otherwise indistinguishable from a healthy follower: it campaigns, backs off and elects nobody, silently, forever. */
func TestLeaderGate_CampaignErrorsReachTheHook(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance := testRuntimeWithContext(runContext)

    observed := make(chan error, 1)

    gate := NewLeaderGateWithOptions(
        &acquireFailingLocker{},
        "campaign",
        20*time.Millisecond,
        LeaderGateOptions{
            RetryInterval: 5 * time.Millisecond,
            OnCampaignError: func(callbackRuntime runtimecontract.Runtime, cause error) {
                select {
                case observed <- cause:
                default:
                }
            },
        },
    )

    go func() {
        _ = gate.Run(runtimeInstance)
    }()

    select {
    case cause := <-observed:
        if nil == cause {
            t.Errorf("the hook must carry the acquire error")
        }
    case <-time.After(2 * time.Second):
        t.Errorf("a gate that can never acquire never reported why")
    }

    cancel()

    if true == gate.IsLeader() {
        t.Fatalf("a gate that never acquired must never claim leadership")
    }
}

/** @info A shutdown cancels the context the backend is called with, so the campaign in flight fails with that cancellation. Reporting it would hand every graceful stop an error indistinguishable from a store outage. */
func TestLeaderGate_ShutdownDoesNotReportACampaignError(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    runtimeInstance := testRuntimeWithContext(runContext)

    reported := make(chan error, 4)

    acquireEntered := make(chan struct{}, 1)

    gate := NewLeaderGateWithOptions(
        &contextSensitiveAcquireLocker{entered: acquireEntered},
        "campaign",
        20*time.Millisecond,
        LeaderGateOptions{
            RetryInterval: 5 * time.Millisecond,
            OnCampaignError: func(callbackRuntime runtimecontract.Runtime, cause error) {
                select {
                case reported <- cause:
                default:
                }
            },
        },
    )

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(runtimeInstance)
    }()

    /* the campaign must be inside the backend call when the shutdown lands, or the loop simply exits before it ever errors */
    select {
    case <-acquireEntered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate never reached Acquire")
    }

    cancel()

    select {
    case runErr := <-done:
        if nil != runErr {
            t.Fatalf("a clean shutdown must return nil, got %v", runErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate did not stop on a cancelled context")
    }

    select {
    case cause := <-reported:
        t.Fatalf("a graceful shutdown reported a campaign error: %v", cause)
    default:
    }
}

/** @info A RetryInterval slower than the one-minute outage cap must not invert into faster-than-healthy retries: doubling the campaign backoff caps at the configured RetryInterval, never below it, so an outage never hammers the store more often than the healthy campaign cadence. */
func TestLeaderGate_CampaignBackoffNeverFasterThanRetryInterval(t *testing.T) {
    retryInterval := 5 * time.Minute

    /* Run seeds the backoff with RetryInterval, so start from there and double repeatedly */
    backoff := retryInterval
    for attempt := 0; attempt < 8; attempt++ {
        backoff = nextCampaignBackoff(backoff, retryInterval)
        if backoff < retryInterval {
            t.Fatalf("outage backoff %v fell below the healthy retry cadence %v after %d doublings", backoff, retryInterval, attempt+1)
        }
    }

    /* a RetryInterval at or under the cap still caps at the cap, never runs away */
    fast := 5 * time.Second
    if capped := nextCampaignBackoff(defaultMaxCampaignBackoff, fast); capped != defaultMaxCampaignBackoff {
        t.Fatalf("expected the backoff to hold at the cap %v, got %v", defaultMaxCampaignBackoff, capped)
    }
}

/** @info An override RefreshInterval slower than half the lease ttl would let the lease lapse before the first renewal, so a second instance could acquire and both report leadership. NewLeaderGateWithOptions must clamp such an override down to the safe derived cadence (ttl/2). */
func TestLeaderGate_RefreshIntervalClampedToHalfTtl(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    ttl := 10 * time.Second
    gate := NewLeaderGateWithOptions(locker, "worker:refresh-clamp", ttl, LeaderGateOptions{
        RefreshInterval: 30 * time.Second,
    })

    if gate.options.RefreshInterval > ttl/2 {
        t.Fatalf("expected the refresh cadence to be clamped to at most %v, got %v", ttl/2, gate.options.RefreshInterval)
    }
}

/* contextSensitiveAcquireLocker fails Acquire with the call's context error, as a real backend does once the context is cancelled. */
type contextSensitiveAcquireLocker struct {
    entered chan struct{}
}

func (instance *contextSensitiveAcquireLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &contextSensitiveAcquireLock{entered: instance.entered}
}

type contextSensitiveAcquireLock struct {
    entered chan struct{}
}

func (instance *contextSensitiveAcquireLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    select {
    case instance.entered <- struct{}{}:
    default:
    }

    /* block inside the backend call, the way a real round trip does, so the shutdown lands while the campaign is in flight */
    <-runtimeInstance.Context().Done()

    return false, exception.NewError("acquire failed", nil, runtimeInstance.Context().Err())
}

func (instance *contextSensitiveAcquireLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *contextSensitiveAcquireLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return nil
}
