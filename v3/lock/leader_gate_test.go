package lock

import (
    "context"
    "strings"
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

    time.Sleep(20 * time.Millisecond)
    if true == second.IsLeader() {
        t.Fatalf("expected the second gate to wait while the first leads")
    }

    firstCancel()
    if runErr := <-firstDone; nil != runErr {
        t.Fatalf("expected a clean shutdown from the first gate, got: %v", runErr)
    }

    waitUntil(t, 2*time.Second, second.IsLeader, "expected the second gate to take over after shutdown")

    secondCancel()
    <-secondDone
}

func TestLeaderGate_DemotesAndReelectsOnRefreshFailure(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())

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

    contender := locker.CreateLock("worker:release", time.Minute)
    acquired, _ := contender.Acquire(testRuntime())
    if false == acquired {
        t.Fatalf("expected the lock to be free right after shutdown")
    }
}

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

func TestLeaderGate_CampaignBackoffNeverFasterThanRetryInterval(t *testing.T) {
    retryInterval := 5 * time.Minute

    backoff := retryInterval
    for attempt := 0; attempt < 8; attempt++ {
        backoff = nextCampaignBackoff(backoff, retryInterval)
        if backoff < retryInterval {
            t.Fatalf("outage backoff %v fell below the healthy retry cadence %v after %d doublings", backoff, retryInterval, attempt+1)
        }
    }

    fast := 5 * time.Second
    if capped := nextCampaignBackoff(defaultMaxCampaignBackoff, fast); capped != defaultMaxCampaignBackoff {
        t.Fatalf("expected the backoff to hold at the cap %v, got %v", defaultMaxCampaignBackoff, capped)
    }
}

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

func TestLeaderGate_RenewsTheLeaseWhileTheElectedHookRuns(t *testing.T) {
    counting := &countingRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGateWithOptions(counting, "worker:elected-hook", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            close(hookEntered)
            <-releaseHook
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    <-hookEntered

    waitUntil(t, 2*time.Second, func() bool {
        return 0 < counting.refreshes()
    }, "expected the lease to be renewed while the elected hook runs")

    close(releaseHook)
    cancel()
    <-done
}

func TestLeaderGate_ElectedHookIsCancelledWhenTheLeaseIsLost(t *testing.T) {
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookReturned := make(chan struct{}, 8)

    gate := NewLeaderGateWithOptions(failing, "worker:elected-cancel", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            <-runtimeInstance.Context().Done()

            select {
            case hookReturned <- struct{}{}:
            default:
            }
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    select {
    case <-hookReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("a failed renewal never stopped the elected hook, so the gate can never demote")
    }

    cancel()
    <-done
}

type countingRefreshLocker struct {
    inner lockcontract.Locker
    count atomic.Int64
}

func (instance *countingRefreshLocker) refreshes() int64 {
    return instance.count.Load()
}

func (instance *countingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &countingRefreshLock{
        locker: instance,
        inner:  instance.inner.CreateLock(name, ttl),
    }
}

type countingRefreshLock struct {
    locker *countingRefreshLocker
    inner  lockcontract.Lock
}

func (instance *countingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *countingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *countingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    instance.locker.count.Add(1)

    return instance.inner.Refresh(runtimeInstance, ttl)
}

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

    <-runtimeInstance.Context().Done()

    return false, exception.NewError("acquire failed", nil, runtimeInstance.Context().Err())
}

func (instance *contextSensitiveAcquireLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func (instance *contextSensitiveAcquireLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return nil
}

type hangingRefreshLocker struct {
    inner lockcontract.Locker
}

func (instance *hangingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &hangingRefreshLock{inner: instance.inner.CreateLock(name, ttl)}
}

type hangingRefreshLock struct {
    inner lockcontract.Lock
}

func (instance *hangingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *hangingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *hangingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    <-runtimeInstance.Context().Done()

    return exception.NewError("lock refresh never answered", nil, runtimeInstance.Context().Err())
}

func TestLeaderGate_ARenewalThatNeverAnswersNeverYieldsTwoLeaders(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())

    ttl := 400 * time.Millisecond
    options := LeaderGateOptions{
        RetryInterval:   20 * time.Millisecond,
        RefreshInterval: 200 * time.Millisecond,
    }

    firstContext, firstCancel := context.WithCancel(context.Background())
    defer firstCancel()
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGateWithOptions(&hangingRefreshLocker{inner: innerLocker}, "worker:split-brain", ttl, options)
    second := NewLeaderGateWithOptions(innerLocker, "worker:split-brain", ttl, options)

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

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if true == first.IsLeader() && true == second.IsLeader() {
            firstCancel()
            secondCancel()
            waitGroup.Wait()

            t.Fatalf("both gates claimed the same lock: the lease lapsed under a renewal that never answered")
        }

        time.Sleep(5 * time.Millisecond)
    }

    firstCancel()
    secondCancel()
    waitGroup.Wait()
}

func TestLeaderGate_LeadershipDropsWhileTheElectedHookIsStillRunning(t *testing.T) {
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGateWithOptions(failing, "worker:hook-unwind", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            close(hookEntered)

            <-releaseHook
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    <-hookEntered

    waitUntil(t, 2*time.Second, func() bool {
        return false == gate.IsLeader()
    }, "expected the gate to stop claiming a lease it lost, without waiting for the elected hook to return")

    close(releaseHook)
    cancel()
    <-done
}

func TestLeaderGate_TheRenewalBudgetNeverOutlivesTheCadenceItSitsInside(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    ttlList := []time.Duration{
        time.Nanosecond,
        time.Microsecond,
        500 * time.Microsecond,
        time.Millisecond,
        2 * time.Millisecond,
        100 * time.Millisecond,
        400 * time.Millisecond,
        30 * time.Second,
        5 * time.Minute,
    }

    for _, ttl := range ttlList {
        gate := NewLeaderGateWithOptions(locker, "worker:budget", ttl, LeaderGateOptions{})

        refreshInterval := gate.options.RefreshInterval
        timeout := resolveRefreshTimeout(refreshInterval)

        if 0 >= timeout {
            t.Fatalf("a ttl of %v derived a non-positive renewal budget %v, which expires before the call is made", ttl, timeout)
        }

        if timeout > refreshInterval {
            t.Fatalf(
                "a ttl of %v derived a renewal budget of %v inside a cadence of %v: the attempt would still be running when its successor started",
                ttl,
                timeout,
                refreshInterval,
            )
        }
    }
}

type countedFailureLocker struct {
    inner        lockcontract.Locker
    failureCount int64
    attempts     atomic.Int64
}

func (instance *countedFailureLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &countedFailureLock{locker: instance, inner: instance.inner.CreateLock(name, ttl)}
}

type countedFailureLock struct {
    locker *countedFailureLocker
    inner  lockcontract.Lock
}

func (instance *countedFailureLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *countedFailureLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *countedFailureLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if instance.locker.attempts.Add(1) <= instance.locker.failureCount {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

func TestLeaderGate_ASingleFailedRenewalDoesNotCostTheTerm(t *testing.T) {
    failing := &countedFailureLocker{inner: NewInMemoryLocker(clock.NewSystemClock()), failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:blip", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lostCount.Add(1)
        },
    })

    done := make(chan struct{})
    go func() {
        _ = gate.Run(testRuntimeWithContext(runContext))
        close(done)
    }()

    select {
    case <-elected:
    case <-time.After(2 * time.Second):
        t.Fatal("the gate never became leader")
    }

    time.Sleep(200 * time.Millisecond)

    if 0 != lostCount.Load() {
        t.Fatalf("one dropped renewal ended the term %d time(s); the lease had a full minute left and no other instance could have taken the lock", lostCount.Load())
    }

    if false == gate.IsLeader() {
        t.Fatal("the gate stopped claiming leadership over a lease that is still valid")
    }

    cancel()
    <-done
}

func TestLeaderGate_ThresholdConsecutiveFailuresEndTheTermWhileTheLeaseIsStillValid(t *testing.T) {
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan error, 16)
    elected := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:gone", time.Minute, LeaderGateOptions{
        RetryInterval:                 5 * time.Millisecond,
        RefreshInterval:               5 * time.Millisecond,
        MaxConsecutiveRefreshFailures: 3,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lost <- cause
        },
    })

    done := make(chan struct{})
    go func() {
        _ = gate.Run(testRuntimeWithContext(runContext))
        close(done)
    }()

    select {
    case <-elected:
    case <-time.After(2 * time.Second):
        t.Fatal("the gate never became leader")
    }

    select {
    case cause := <-lost:
        if nil == cause {
            t.Fatal("expected the demotion to carry the renewal failure")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("three consecutive failed renewals did not end the term: a gate whose cadence is far denser than its lease would keep working for the whole lease against a store that has plainly gone")
    }

    cancel()
    <-done
}

func TestLeaderGate_ANegativeThresholdLeavesOnlyTheLeaseClock(t *testing.T) {
    gate := NewLeaderGateWithOptions(
        NewInMemoryLocker(clock.NewSystemClock()),
        "worker:lease-only",
        time.Minute,
        LeaderGateOptions{MaxConsecutiveRefreshFailures: -1},
    )

    if false == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("expected the lease clock to answer on its own")
    }

    gate.leaseExpiry.Store(time.Now().Add(time.Minute).UnixNano())

    if true == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("a negative threshold must be off: a thousand failures may not end a term whose lease has a minute left")
    }
}

func TestLeaderGate_TheDefaultThresholdIsUnreachableAtTheDefaultCadence(t *testing.T) {
    ttl := time.Minute

    gate := NewLeaderGateWithOptions(NewInMemoryLocker(clock.NewSystemClock()), "worker:default", ttl, LeaderGateOptions{})

    if defaultMaxConsecutiveRefreshFailures != gate.options.MaxConsecutiveRefreshFailures {
        t.Fatalf("expected the default threshold, got %d", gate.options.MaxConsecutiveRefreshFailures)
    }

    thresholdWindow := time.Duration(gate.options.MaxConsecutiveRefreshFailures) * gate.options.RefreshInterval
    if thresholdWindow <= ttl {
        t.Fatalf(
            "the default threshold fires after %s, inside a lease of %s: it would decide instead of the lease clock in an ordinary deployment",
            thresholdWindow,
            ttl,
        )
    }
}

type alternatingRefreshLocker struct {
    inner    lockcontract.Locker
    attempts atomic.Int64
}

func (instance *alternatingRefreshLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return &alternatingRefreshLock{locker: instance, inner: instance.inner.CreateLock(name, ttl)}
}

type alternatingRefreshLock struct {
    locker *alternatingRefreshLocker
    inner  lockcontract.Lock
}

func (instance *alternatingRefreshLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *alternatingRefreshLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *alternatingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 == instance.locker.attempts.Add(1)%2 {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

func TestLeaderGate_ScatteredFailuresNeverAccumulateIntoADemotion(t *testing.T) {
    failing := &alternatingRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:lossy", time.Minute, LeaderGateOptions{
        RetryInterval:                 5 * time.Millisecond,
        RefreshInterval:               5 * time.Millisecond,
        MaxConsecutiveRefreshFailures: 3,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lostCount.Add(1)
        },
    })

    done := make(chan struct{})
    go func() {
        _ = gate.Run(testRuntimeWithContext(runContext))
        close(done)
    }()

    select {
    case <-elected:
    case <-time.After(2 * time.Second):
        t.Fatal("the gate never became leader")
    }

    time.Sleep(300 * time.Millisecond)

    if 0 != lostCount.Load() {
        t.Fatalf(
            "scattered failures ended the term %d time(s) after %d renewal attempts: the consecutive counter is not being cleared by the renewals that land",
            lostCount.Load(),
            failing.attempts.Load(),
        )
    }

    cancel()
    <-done
}

func TestLeaderGate_SessionModeToleratesTransientProbeFailures(t *testing.T) {
    failing := &countedFailureLocker{inner: NewInMemoryLocker(clock.NewSystemClock()), failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:session-blip", 0, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 20 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lostCount.Add(1)
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case <-elected:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate was never elected")
    }

    waitUntil(t, 2*time.Second, func() bool { return 3 <= failing.attempts.Load() }, "expected several probes to land")

    if 0 != lostCount.Load() {
        t.Fatalf("expected a single failed probe not to cost a session-mode term, got %d losses", lostCount.Load())
    }

    if false == gate.IsLeader() {
        t.Fatalf("expected the gate to still lead after a transient probe failure")
    }
}

func TestLeaderGate_SessionModePersistentProbeFailuresEndTheTerm(t *testing.T) {
    failing := &countedFailureLocker{inner: NewInMemoryLocker(clock.NewSystemClock()), failureCount: 1 << 30}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan struct{}, 16)

    gate := NewLeaderGateWithOptions(failing, "worker:session-gone", 0, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lost <- struct{}{}
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case <-lost:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the default threshold to end a session-mode term against a store that is gone")
    }
}

func TestLeaderGate_PanickingOnElectedHookIsShieldedAndLogged(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    gate := NewLeaderGateWithOptions(locker, "worker:hook-panics", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnElected: func(electedRuntime runtimecontract.Runtime) {
            panic(exception.NewError("hook exploded", nil, nil))
        },
    })

    runDone := make(chan struct{})
    go func() {
        _ = gate.Run(runtimeInstance)
        close(runDone)
    }()

    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate hook panicked") }, "expected the hook panic to be logged")

    if false == gate.IsLeader() {
        t.Fatalf("expected the gate to keep leading after a recovered hook panic")
    }

    cancel()

    select {
    case <-runDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate did not shut down cleanly after the recovered panic")
    }

    acquired, acquireErr := locker.CreateLock("worker:hook-panics", time.Minute).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to be released on shutdown, got acquired=%v err=%v", acquired, acquireErr)
    }
}

func TestLeaderGate_CampaignErrorIsLoggedWhenNoHookIsWired(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    gate := NewLeaderGateWithOptions(&acquireFailingLocker{}, "worker:no-hook", time.Minute, fastGateOptions())

    go gate.Run(runtimeInstance)

    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate campaign failed") }, "expected the failed campaign to be logged")
}

func TestLeaderGate_CampaignErrorHookReplacesTheDefaultRecord(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    hookCalls := make(chan error, 16)

    options := fastGateOptions()
    options.OnCampaignError = func(hookRuntime runtimecontract.Runtime, cause error) {
        hookCalls <- cause
    }

    gate := NewLeaderGateWithOptions(&acquireFailingLocker{}, "worker:with-hook", time.Minute, options)

    go gate.Run(runtimeInstance)

    select {
    case cause := <-hookCalls:
        if nil == cause {
            t.Fatalf("expected the hook to receive the campaign error")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the campaign error hook was never called")
    }

    if true == logger.hasMessageContaining("leader gate campaign failed") {
        t.Fatalf("expected the wired hook to replace the default record, not add to it")
    }
}

func TestLeaderGate_PanickingRefreshDemotesInsteadOfKillingTheProcess(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan error, 16)

    gate := NewLeaderGateWithOptions(&panickingRefreshLocker{}, "worker:refresh-panics", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lost <- cause
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case cause := <-lost:
        if nil == cause || false == strings.Contains(cause.Error(), "leader gate refresh panicked") {
            t.Fatalf("expected the recovered refresh panic as the lost cause, got %v", cause)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the panicking refresh to demote the term")
    }
}

func TestLeaderGate_IsLeaderAnswersFromTheAcquireLeaseBeforeAnyRenewal(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    leaderAtElection := make(chan bool, 1)

    var gate *LeaderGate
    gate = NewLeaderGateWithOptions(NewInMemoryLocker(clock.NewSystemClock()), "worker:first-window", time.Minute, LeaderGateOptions{
        RetryInterval: 5 * time.Millisecond,
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            leaderAtElection <- gate.IsLeader()
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case isLeader := <-leaderAtElection:
        if false == isLeader {
            t.Fatalf("expected IsLeader to answer from the acquire-dated lease before any renewal")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate was never elected")
    }
}
