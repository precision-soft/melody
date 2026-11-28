package lock

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync"
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

type switchableRefreshLocker struct {
    inner lockcontract.Locker
    fail  atomic.Bool
}

func (instance *switchableRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &switchableRefreshLock{
        locker: instance,
        inner:  mustCreateLock(instance.inner.CreateLock(name, options)),
    }, nil
}

type switchableRefreshLock struct {
    locker *switchableRefreshLocker
    inner  lockcontract.Lock
}

func (instance *switchableRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *switchableRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *switchableRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if true == instance.locker.fail.Load() {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, options)
}

func fastGateConfig(ttl bound.Duration) LeaderGateConfig {
    return LeaderGateConfig{
        Ttl:             ttl,
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    firstContext, firstCancel := context.WithCancel(context.Background())
    defer firstCancel()
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGate(locker, "worker:leader", fastGateConfig(bound.Of(time.Minute)))
    second := NewLeaderGate(locker, "worker:leader", fastGateConfig(bound.Of(time.Minute)))

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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    firstContext, firstCancel := context.WithCancel(context.Background())
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGate(locker, "worker:failover", fastGateConfig(bound.Of(time.Minute)))
    second := NewLeaderGate(locker, "worker:failover", fastGateConfig(bound.Of(time.Minute)))

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
    innerLocker := NewInMemoryLocker(InMemoryLockerConfig{})

    failing := &switchableRefreshLocker{inner: innerLocker}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCauses []error
    var lostMutex sync.Mutex
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:lease", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())

    gate := NewLeaderGate(locker, "worker:release", fastGateConfig(bound.Of(time.Hour)))

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

    contender := mustCreateLock(locker.CreateLock("worker:release", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)}))
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

    gate := NewLeaderGate(
        &acquireFailingLocker{},
        "campaign",
        LeaderGateConfig{
            Ttl:           bound.Of(20 * time.Millisecond),
            RetryInterval: bound.Of(5 * time.Millisecond),
            OnCampaignError: func(callbackRuntime containercontract.Runtime, cause error) {
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

    gate := NewLeaderGate(
        &contextSensitiveAcquireLocker{entered: acquireEntered},
        "campaign",
        LeaderGateConfig{
            Ttl:           bound.Of(20 * time.Millisecond),
            RetryInterval: bound.Of(5 * time.Millisecond),
            OnCampaignError: func(callbackRuntime containercontract.Runtime, cause error) {
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

    /* Run seeds the backoff with RetryInterval, so start from there and double repeatedly */
    backoff := retryInterval
    for attempt := 0; attempt < 8; attempt++ {
        backoff = nextCampaignBackoff(backoff, retryInterval, leaderGateMaxCampaignBackoffRule.Default)
        if backoff < retryInterval {
            t.Fatalf("outage backoff %v fell below the healthy retry cadence %v after %d doublings", backoff, retryInterval, attempt+1)
        }
    }

    fast := 5 * time.Second
    maxCampaignBackoff := leaderGateMaxCampaignBackoffRule.Default
    if capped := nextCampaignBackoff(maxCampaignBackoff, fast, maxCampaignBackoff); capped != maxCampaignBackoff {
        t.Fatalf("expected the backoff to hold at the cap %v, got %v", maxCampaignBackoff, capped)
    }
}

func TestLeaderGate_ARefreshIntervalAboveHalfTheTtlIsRefused(t *testing.T) {
    ttl := 10 * time.Second

    var recovered any
    func() {
        defer func() { recovered = recover() }()

        NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:refresh-ceiling", LeaderGateConfig{
            Ttl:             bound.Of(ttl),
            RefreshInterval: bound.Of(ttl/2 + time.Millisecond),
        })
    }()

    refusal, isMelodyErr := recovered.(*exception.Error)
    if false == isMelodyErr {
        t.Fatalf("expected the refresh interval to be refused with a melody error, got %#v", recovered)
    }

    if "LeaderGateConfig.RefreshInterval" != refusal.Context()["field"] || (ttl/2).String() != refusal.Context()["maximum"] {
        t.Fatalf("expected the refusal to name the field and the ceiling, got %v", refusal.Context())
    }

    gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:refresh-ceiling", LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RefreshInterval: bound.Of(ttl / 2),
    })
    if ttl/2 != gate.settings.refreshInterval {
        t.Fatalf("expected half the ttl to be admitted, got %v", gate.settings.refreshInterval)
    }
}

func TestLeaderGate_RenewsTheLeaseWhileTheElectedHookRuns(t *testing.T) {
    counting := &countingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGate(counting, "worker:elected-hook", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
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
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookReturned := make(chan struct{}, 8)

    gate := NewLeaderGate(failing, "worker:elected-cancel", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
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

func (instance *countingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &countingRefreshLock{
        locker: instance,
        inner:  mustCreateLock(instance.inner.CreateLock(name, options)),
    }, nil
}

type countingRefreshLock struct {
    locker *countingRefreshLocker
    inner  lockcontract.Lock
}

func (instance *countingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *countingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *countingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    instance.locker.count.Add(1)

    return instance.inner.Refresh(runtimeInstance, options)
}

type contextSensitiveAcquireLocker struct {
    entered chan struct{}
}

func (instance *contextSensitiveAcquireLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &contextSensitiveAcquireLock{entered: instance.entered}, nil
}

type contextSensitiveAcquireLock struct {
    entered chan struct{}
}

func (instance *contextSensitiveAcquireLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    select {
    case instance.entered <- struct{}{}:
    default:
    }

    <-runtimeInstance.Context().Done()

    return false, exception.NewError("acquire failed", nil, runtimeInstance.Context().Err())
}

func (instance *contextSensitiveAcquireLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *contextSensitiveAcquireLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

type hangingRefreshLocker struct {
    inner lockcontract.Locker
}

func (instance *hangingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &hangingRefreshLock{inner: mustCreateLock(instance.inner.CreateLock(name, options))}, nil
}

type hangingRefreshLock struct {
    inner lockcontract.Lock
}

func (instance *hangingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *hangingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *hangingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    <-runtimeInstance.Context().Done()

    return exception.NewError("lock refresh never answered", nil, runtimeInstance.Context().Err())
}

func TestLeaderGate_ARenewalThatNeverAnswersNeverYieldsTwoLeaders(t *testing.T) {
    innerLocker := NewInMemoryLocker(InMemoryLockerConfig{})

    ttl := 400 * time.Millisecond
    options := LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RetryInterval:   bound.Of(20 * time.Millisecond),
        RefreshInterval: bound.Of(200 * time.Millisecond),
    }

    firstContext, firstCancel := context.WithCancel(context.Background())
    defer firstCancel()
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    first := NewLeaderGate(&hangingRefreshLocker{inner: innerLocker}, "worker:split-brain", options)
    second := NewLeaderGate(innerLocker, "worker:split-brain", options)

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

    /* long enough for the lease to lapse several times over under the renewal that never answers */
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
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGate(failing, "worker:hook-unwind", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    ttlList := []time.Duration{
        2 * time.Millisecond,
        100 * time.Millisecond,
        400 * time.Millisecond,
        30 * time.Second,
        5 * time.Minute,
    }

    for _, ttl := range ttlList {
        gate := NewLeaderGate(locker, "worker:budget", LeaderGateConfig{Ttl: bound.Of(ttl)})

        refreshInterval := gate.settings.refreshInterval
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

func (instance *countedFailureLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &countedFailureLock{locker: instance, inner: mustCreateLock(instance.inner.CreateLock(name, options))}, nil
}

type countedFailureLock struct {
    locker *countedFailureLocker
    inner  lockcontract.Lock
}

func (instance *countedFailureLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *countedFailureLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *countedFailureLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if instance.locker.attempts.Add(1) <= instance.locker.failureCount {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, options)
}

func TestLeaderGate_ASingleFailedRenewalDoesNotCostTheTerm(t *testing.T) {
    failing := &countedFailureLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{}), failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:blip", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
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

    /* well past several cadences, so a gate that leaves on the first failure has certainly done so by now */
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
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan error, 16)
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:gone", LeaderGateConfig{
        Ttl:                           bound.Of(time.Minute),
        RetryInterval:                 bound.Of(5 * time.Millisecond),
        RefreshInterval:               bound.Of(5 * time.Millisecond),
        MaxConsecutiveRefreshFailures: bound.Of(3),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
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

func TestLeaderGate_AnUnlimitedThresholdLeavesOnlyTheLeaseClock(t *testing.T) {
    gate := NewLeaderGate(
        NewInMemoryLocker(InMemoryLockerConfig{}),
        "worker:lease-only",
        LeaderGateConfig{Ttl: bound.Of(time.Minute), MaxConsecutiveRefreshFailures: bound.UnlimitedCount()},
    )

    if false == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("expected the lease clock to answer on its own")
    }

    gate.leaseExpiry.Store(time.Now().Add(time.Minute).UnixNano())

    if true == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("an unlimited threshold must be off: a thousand failures may not end a term whose lease has a minute left")
    }
}

func TestLeaderGate_TheDefaultThresholdIsUnreachableAtTheDefaultCadence(t *testing.T) {
    ttl := time.Minute

    gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:default", LeaderGateConfig{Ttl: bound.Of(ttl)})

    if leaderGateMaxConsecutiveRefreshFailuresRule.Default != gate.settings.maxConsecutiveRefreshFailures {
        t.Fatalf("expected the default threshold, got %d", gate.settings.maxConsecutiveRefreshFailures)
    }

    thresholdWindow := time.Duration(gate.settings.maxConsecutiveRefreshFailures) * gate.settings.refreshInterval
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

func (instance *alternatingRefreshLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &alternatingRefreshLock{locker: instance, inner: mustCreateLock(instance.inner.CreateLock(name, options))}, nil
}

type alternatingRefreshLock struct {
    locker *alternatingRefreshLocker
    inner  lockcontract.Lock
}

func (instance *alternatingRefreshLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *alternatingRefreshLock) Release(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Release(runtimeInstance)
}

func (instance *alternatingRefreshLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    if 0 == instance.locker.attempts.Add(1)%2 {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, options)
}

func TestLeaderGate_ScatteredFailuresNeverAccumulateIntoADemotion(t *testing.T) {
    failing := &alternatingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:lossy", LeaderGateConfig{
        Ttl:                           bound.Of(time.Minute),
        RetryInterval:                 bound.Of(5 * time.Millisecond),
        RefreshInterval:               bound.Of(5 * time.Millisecond),
        MaxConsecutiveRefreshFailures: bound.Of(3),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
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
    failing := &countedFailureLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}, failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:session-blip", LeaderGateConfig{
        Ttl:             bound.UnlimitedDuration(),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(20 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            elected <- struct{}{}
        },
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
            lostCount.Add(1)
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case <-elected:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate was never elected")
    }

    /* wait long enough for the single failing probe and several healthy ones to land */
    waitUntil(t, 2*time.Second, func() bool { return 3 <= failing.attempts.Load() }, "expected several probes to land")

    if 0 != lostCount.Load() {
        t.Fatalf("expected a single failed probe not to cost a session-mode term, got %d losses", lostCount.Load())
    }

    if false == gate.IsLeader() {
        t.Fatalf("expected the gate to still lead after a transient probe failure")
    }
}

func TestLeaderGate_SessionModePersistentProbeFailuresEndTheTerm(t *testing.T) {
    failing := &countedFailureLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}, failureCount: 1 << 30}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:session-gone", LeaderGateConfig{
        Ttl:             bound.UnlimitedDuration(),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
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
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    gate := NewLeaderGate(locker, "worker:hook-panics", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime containercontract.Runtime) {
            panic(exception.NewError("hook exploded", nil, nil))
        },
    })

    runDone := make(chan struct{})
    go func() {
        _ = gate.Run(runtimeInstance)
        close(runDone)
    }()

    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate hook panicked") }, "expected the hook panic to be logged")

    waitUntil(t, 2*time.Second, func() bool { return false == gate.IsLeader() }, "expected the term to end after the recovered hook panic")

    cancel()

    select {
    case <-runDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate did not shut down cleanly after the recovered panic")
    }

    acquired, acquireErr := mustCreateLock(locker.CreateLock("worker:hook-panics", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)})).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to be released on shutdown, got acquired=%v err=%v", acquired, acquireErr)
    }
}

func TestLeaderGate_CampaignErrorIsLoggedWhenNoHookIsWired(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    gate := NewLeaderGate(&acquireFailingLocker{}, "worker:no-hook", fastGateConfig(bound.Of(time.Minute)))

    go gate.Run(runtimeInstance)

    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate campaign failed") }, "expected the failed campaign to be logged")
}

func TestLeaderGate_CampaignErrorHookReplacesTheDefaultRecord(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    hookCalls := make(chan error, 16)

    options := fastGateConfig(bound.Of(time.Minute))
    options.OnCampaignError = func(hookRuntime containercontract.Runtime, cause error) {
        hookCalls <- cause
    }

    gate := NewLeaderGate(&acquireFailingLocker{}, "worker:with-hook", options)

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

    gate := NewLeaderGate(&panickingRefreshLocker{}, "worker:refresh-panics", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnLost: func(runtimeInstance containercontract.Runtime, cause error) {
            lost <- cause
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    select {
    case cause := <-lost:
        if nil == cause || false == strings.Contains(cause.Error(), "leader gate refresh panicked") {
            t.Fatalf("expected the recovered refresh panic as the lost cause, got %v", cause)
        }

        if false == errors.Is(cause, errPanickingRefreshBackend) {
            t.Fatalf("expected the panic value as the cause of the refresh failure, got %v", cause)
        }

        if panicStack := chainContextValue(cause, "leader gate refresh panicked", "panicStack"); false == strings.Contains(panicStack, "panickingRefreshLock") {
            t.Fatalf("expected the stack the refresh panicked on, got %q", panicStack)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the panicking refresh to demote the term")
    }
}

func TestLeaderGate_IsLeaderAnswersFromTheAcquireLeaseBeforeAnyRenewal(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    leaderAtElection := make(chan bool, 1)

    /* the cadence stays at its default, half a one-minute ttl, so no renewal lands before the assertion */
    var gate *LeaderGate
    gate = NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:first-window", LeaderGateConfig{
        Ttl:           bound.Of(time.Minute),
        RetryInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
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

func TestLeaderGate_APanickingOnElectedEndsTheTermAndReleasesTheLock(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, _ := runtimeWithRecordingLogger(runContext)

    lostCauses := make(chan error, 4)
    gate := NewLeaderGate(locker, "worker:hook-panics-term", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(time.Second),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime containercontract.Runtime) {
            panic(exception.NewError("hook exploded", nil, nil))
        },
        OnLost: func(lostRuntime containercontract.Runtime, cause error) {
            lostCauses <- cause
        },
    })

    go func() { _ = gate.Run(runtimeInstance) }()

    var lostCause error
    select {
    case lostCause = <-lostCauses:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected OnLost to receive the panicking hook as a lost term")
    }

    if nil == lostCause {
        t.Fatalf("expected the lost term to carry a cause")
    }

    if false == strings.Contains(lostCause.Error(), "leader gate hook panicked") {
        t.Fatalf("expected the lost cause to name the hook panic, got %v", lostCause)
    }

    if "OnElected" != exception.LogContext(lostCause, exception.LogContextOptions{})["hook"] {
        t.Fatalf("expected the lost cause to name the hook, got %v", exception.LogContext(lostCause, exception.LogContextOptions{}))
    }

    if true == gate.IsLeader() {
        t.Fatalf("expected the gate to have left its term")
    }

    acquired, acquireErr := mustCreateLock(locker.CreateLock("worker:hook-panics-term", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)})).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to be released with the ended term, got acquired=%v err=%v", acquired, acquireErr)
    }
}

func TestLeaderGate_APanickingOnElectedIsCampaignedAgainAfterRetryInterval(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, _ := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:hook-panics-again", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime containercontract.Runtime) {
            elections.Add(1)
            panic("hook exploded")
        },
    })

    go func() { _ = gate.Run(runtimeInstance) }()

    waitUntil(t, 2*time.Second, func() bool { return 2 <= elections.Load() }, "expected the gate to campaign again after the panicking term ended")
}

func TestLeaderGate_APanickingOnLostDoesNotStopTheCampaigns(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:lost-panics", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime containercontract.Runtime) {
            elections.Add(1)
            panic("elected exploded")
        },
        OnLost: func(lostRuntime containercontract.Runtime, cause error) {
            panic("lost exploded")
        },
    })

    go func() { _ = gate.Run(runtimeInstance) }()

    waitUntil(t, 2*time.Second, func() bool { return 2 <= elections.Load() }, "expected the campaigns to go on past a panicking OnLost")

    if false == logger.hasMessageContaining("leader gate hook panicked") {
        t.Fatalf("expected the panicking hooks to be journaled")
    }
}

type nilableTestScope struct {
    containercontract.Scope
}

type typedNilScopeRuntime struct {
    ctx context.Context
}

func (instance *typedNilScopeRuntime) Context() context.Context       { return instance.ctx }
func (instance *typedNilScopeRuntime) Scope() containercontract.Scope { return (*nilableTestScope)(nil) }
func (instance *typedNilScopeRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

func TestLeaderGateRun_RefusesATypedNilScopeBeforeCampaigning(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})
    gate := NewLeaderGate(locker, "worker:typed-nil-scope", LeaderGateConfig{Ttl: bound.Of(time.Minute)})

    testhelper.AssertPanicsWithError(t, func() {
        _ = gate.Run(&typedNilScopeRuntime{ctx: context.Background()})
    }, "leader gate runtime scope is nil")

    acquired, acquireErr := mustCreateLock(locker.CreateLock("worker:typed-nil-scope", lockcontract.CreateLockOptions{Ttl: bound.Of(time.Minute)})).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected no lock to be held by the refused run, got acquired=%v err=%v", acquired, acquireErr)
    }
}

func handoverMarginUnder(t *testing.T, innerLocker lockcontract.Locker, firstLocker lockcontract.Locker, ttl time.Duration) time.Duration {
    t.Helper()

    firstContext, firstCancel := context.WithCancel(context.Background())
    defer firstCancel()
    secondContext, secondCancel := context.WithCancel(context.Background())
    defer secondCancel()

    firstElected := make(chan struct{})
    firstStopped := make(chan time.Time, 1)
    secondElected := make(chan time.Time, 1)
    releaseFirstHook := make(chan struct{})

    first := NewLeaderGate(firstLocker, "worker:handover", LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RetryInterval:   bound.Of(time.Minute),
        RefreshInterval: bound.Of(ttl / 2),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            close(firstElected)
            <-runtimeInstance.Context().Done()
            firstStopped <- time.Now()
            <-releaseFirstHook
        },
    })

    second := NewLeaderGate(innerLocker, "worker:handover", LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(ttl / 2),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            select {
            case secondElected <- time.Now():
            default:
            }

            <-runtimeInstance.Context().Done()
        },
    })

    var waitGroup sync.WaitGroup
    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        _ = first.Run(testRuntimeWithContext(firstContext))
    }()

    <-firstElected

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        _ = second.Run(testRuntimeWithContext(secondContext))
    }()

    stoppedAt := <-firstStopped
    electedAt := <-secondElected

    close(releaseFirstHook)
    firstCancel()
    secondCancel()
    waitGroup.Wait()

    return electedAt.Sub(stoppedAt)
}

/* cut at the demotion instant, the stop lands a quarter of the ttl before the lapse */
func TestLeaderGate_AHungRenewalStopsTheElectedHookBeforeASecondGateIsElected(t *testing.T) {
    innerLocker := NewInMemoryLocker(InMemoryLockerConfig{})
    ttl := 400 * time.Millisecond

    if margin := handoverMarginUnder(t, innerLocker, &hangingRefreshLocker{inner: innerLocker}, ttl); margin < ttl/8 {
        t.Fatalf("expected the first hook told to stop at least an eighth of the ttl before the second gate was elected, stopped %v before", margin)
    }
}

func TestLeaderGate_AFailingRenewalStopsTheElectedHookBeforeASecondGateIsElected(t *testing.T) {
    innerLocker := NewInMemoryLocker(InMemoryLockerConfig{})
    ttl := 400 * time.Millisecond

    failing := &switchableRefreshLocker{inner: innerLocker}
    failing.fail.Store(true)

    if margin := handoverMarginUnder(t, innerLocker, failing, ttl); margin < ttl/8 {
        t.Fatalf("expected the first hook told to stop at least an eighth of the ttl before the second gate was elected, stopped %v before", margin)
    }
}

type parkingReleaseLocker struct {
    inner          lockcontract.Locker
    releaseEntered chan struct{}
    releaseProceed chan struct{}
    enteredOnce    sync.Once
}

func (instance *parkingReleaseLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &parkingReleaseLock{locker: instance, inner: mustCreateLock(instance.inner.CreateLock(name, options))}, nil
}

type parkingReleaseLock struct {
    locker *parkingReleaseLocker
    inner  lockcontract.Lock
}

func (instance *parkingReleaseLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *parkingReleaseLock) Release(runtimeInstance containercontract.Runtime) error {
    instance.locker.enteredOnce.Do(func() {
        close(instance.locker.releaseEntered)
    })
    <-instance.locker.releaseProceed

    return instance.inner.Release(runtimeInstance)
}

func (instance *parkingReleaseLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return instance.inner.Refresh(runtimeInstance, options)
}

func TestLeaderGate_LeavesTheTermBeforeTheReleaseReachesTheStore(t *testing.T) {
    locker := &parkingReleaseLocker{
        inner:          NewInMemoryLocker(InMemoryLockerConfig{}),
        releaseEntered: make(chan struct{}),
        releaseProceed: make(chan struct{}),
    }

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    elected := make(chan struct{})
    gate := NewLeaderGate(locker, "worker:leave-then-release", LeaderGateConfig{
        Ttl:           bound.Of(time.Minute),
        RetryInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            close(elected)
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    <-elected
    cancel()
    <-locker.releaseEntered

    leadingDuringTheRelease := gate.IsLeader()

    close(locker.releaseProceed)
    <-done

    if true == leadingDuringTheRelease {
        t.Fatalf("expected the gate to have left its term while its release was still in flight")
    }
}

func TestLeaderGate_ARenewalIgnoringItsContextStopsTheElectedHookAtTheDemotionInstant(t *testing.T) {
    ttl := 400 * time.Millisecond
    locker := &contextIgnoringRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{}), delay: 180 * time.Millisecond}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    stoppedAfter := make(chan time.Duration, 1)
    gate := NewLeaderGate(locker, "worker:context-ignoring", LeaderGateConfig{
        Ttl:           bound.Of(ttl),
        RetryInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance containercontract.Runtime) {
            electedAt := time.Now()
            select {
            case <-runtimeInstance.Context().Done():
                stoppedAfter <- time.Since(electedAt)
            case <-time.After(3 * time.Second):
                stoppedAfter <- -1
            }
        },
    })

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    stopped := <-stoppedAfter
    cancel()
    <-done

    if 0 > stopped || stopped > ttl-ttl/8 {
        t.Fatalf("expected the elected hook told to stop at least an eighth of the ttl before the lapse, stopped after %v", stopped)
    }
}

func TestLeaderGate_AnOnElectedPanicWithNoOnLostIsJournaledWithItsStackOnce(t *testing.T) {
    locker := NewInMemoryLocker(InMemoryLockerConfig{})

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:hook-panics-once", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(time.Hour),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime containercontract.Runtime) {
            elections.Add(1)
            panic("elected exploded")
        },
    })

    go func() { _ = gate.Run(runtimeInstance) }()

    waitUntil(t, 2*time.Second, func() bool {
        return logger.hasMessageContaining("leader gate lost the term after a hook panicked")
    }, "expected the lost term named after the hook")

    logger.mutex.Lock()
    defer logger.mutex.Unlock()

    stackRecords := 0
    for _, entry := range logger.records {
        if true == strings.Contains(fmt.Sprint(entry.context), "panicStack") {
            stackRecords++
        }
    }

    if 1 != stackRecords {
        t.Fatalf("expected the panic stack journaled once, got %d records carrying it", stackRecords)
    }
}

func TestNewLeaderGate_TheZeroConfigurationDerivesItsDefaultsFromAThirtySecondLease(t *testing.T) {
    gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:zero-config", LeaderGateConfig{})

    if 30*time.Second != gate.settings.ttl || true == gate.settings.sessionMode {
        t.Fatalf("expected a 30s lease, got %s session=%v", gate.settings.ttl, gate.settings.sessionMode)
    }

    if 15*time.Second != gate.settings.retryInterval || 15*time.Second != gate.settings.refreshInterval {
        t.Fatalf("expected the intervals at half the lease, got retry %s refresh %s", gate.settings.retryInterval, gate.settings.refreshInterval)
    }

    if 3 != gate.settings.maxConsecutiveRefreshFailures || time.Minute != gate.settings.maxCampaignBackoff {
        t.Fatalf("expected a threshold of 3 and a 1m backoff cap, got %d and %s", gate.settings.maxConsecutiveRefreshFailures, gate.settings.maxCampaignBackoff)
    }

    if ttl, isSet := gate.lockTtl().Value(); false == isSet || 30*time.Second != ttl {
        t.Fatalf("expected each campaign to hand the locker the 30s lease, got %s", gate.lockTtl())
    }
}

func TestNewLeaderGate_AnUnlimitedTtlCampaignsForASessionLockProbedEveryFifteenSeconds(t *testing.T) {
    gate := NewLeaderGate(&sessionStyleLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}, "worker:session-config", LeaderGateConfig{Ttl: bound.UnlimitedDuration()})

    if false == gate.settings.sessionMode || false == gate.lockTtl().IsUnlimited() {
        t.Fatalf("expected session mode and an unlimited ttl handed to the locker, got %v and %s", gate.settings.sessionMode, gate.lockTtl())
    }

    if time.Second != gate.settings.retryInterval || defaultSessionProbeInterval != gate.settings.refreshInterval {
        t.Fatalf("expected a 1s retry and the 15s probe, got retry %s refresh %s", gate.settings.retryInterval, gate.settings.refreshInterval)
    }
}

func TestNewLeaderGate_AShortLeaseRetriesNoFasterThanOnceASecond(t *testing.T) {
    gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:short-lease", LeaderGateConfig{Ttl: bound.Of(time.Second)})

    if time.Second != gate.settings.retryInterval || 500*time.Millisecond != gate.settings.refreshInterval {
        t.Fatalf("expected a 1s retry floor and a 500ms refresh, got retry %s refresh %s", gate.settings.retryInterval, gate.settings.refreshInterval)
    }
}

func TestNewLeaderGate_TheSetFieldsReachTheGate(t *testing.T) {
    gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:set-config", LeaderGateConfig{
        Ttl:                           bound.Of(time.Minute),
        RetryInterval:                 bound.Of(2 * time.Millisecond),
        RefreshInterval:               bound.Of(10 * time.Second),
        MaxConsecutiveRefreshFailures: bound.Of(7),
        MaxCampaignBackoff:            bound.Of(2 * time.Minute),
    })

    if 2*time.Millisecond != gate.settings.retryInterval || 10*time.Second != gate.settings.refreshInterval || 7 != gate.settings.maxConsecutiveRefreshFailures || 2*time.Minute != gate.settings.maxCampaignBackoff {
        t.Fatalf("expected the set fields as they were declared, got %+v", gate.settings)
    }

    if capped := nextCampaignBackoff(time.Minute+time.Second, gate.settings.retryInterval, gate.settings.maxCampaignBackoff); 2*time.Minute != capped {
        t.Fatalf("expected the doubling capped at the declared 2m, got %s", capped)
    }
}

func TestNewLeaderGate_RefusesAConfigurationTheRulesRefuseNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        config LeaderGateConfig
        field  string
    }{
        {"a ttl under 2ms", LeaderGateConfig{Ttl: bound.Of(2*time.Millisecond - time.Microsecond)}, "LeaderGateConfig.Ttl"},
        {"an unlimited retry", LeaderGateConfig{RetryInterval: bound.UnlimitedDuration()}, "LeaderGateConfig.RetryInterval"},
        {"a refresh under 1ms", LeaderGateConfig{RefreshInterval: bound.Of(time.Microsecond)}, "LeaderGateConfig.RefreshInterval"},
        {"a threshold of 0", LeaderGateConfig{MaxConsecutiveRefreshFailures: bound.Of(0)}, "LeaderGateConfig.MaxConsecutiveRefreshFailures"},
        {"an unlimited backoff", LeaderGateConfig{MaxCampaignBackoff: bound.UnlimitedDuration()}, "LeaderGateConfig.MaxCampaignBackoff"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            var recovered any
            func() {
                defer func() { recovered = recover() }()

                NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:refused", testCase.config)
            }()

            refusal, isMelodyErr := recovered.(*exception.Error)
            if false == isMelodyErr || testCase.field != refusal.Context()["field"] {
                t.Fatalf("expected a panic naming %s, got %#v", testCase.field, recovered)
            }
        })
    }
}

func TestNewLeaderGate_AnUnlimitedThresholdBesideAnUnlimitedTtlIsRefusedNamingBoth(t *testing.T) {
    var recovered any
    func() {
        defer func() { recovered = recover() }()

        NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:no-signal", LeaderGateConfig{
            Ttl:                           bound.UnlimitedDuration(),
            MaxConsecutiveRefreshFailures: bound.UnlimitedCount(),
        })
    }()

    refusal, isMelodyErr := recovered.(*exception.Error)
    if false == isMelodyErr || "LeaderGateConfig.MaxConsecutiveRefreshFailures" != refusal.Context()["field"] || "LeaderGateConfig.Ttl" != refusal.Context()["besideField"] {
        t.Fatalf("expected a panic naming both fields, got %#v", recovered)
    }
}

func TestNewLeaderGate_RefusesAnUnlimitedTtlOverALeaseLockerNamingTheField(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "an unlimited leader gate ttl needs a session-style locker" != recoveredErr.Error() {
            t.Fatalf("expected the refusal, got %v", recoveredErr)
        }

        if false == errors.Is(recoveredErr, lockcontract.ErrUnlimitedTtlRefused) {
            t.Fatalf("expected ErrUnlimitedTtlRefused as the cause, got %v", recoveredErr)
        }

        if "LeaderGateConfig.Ttl" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the refusal to name the field, got %v", exception.LogContext(recoveredErr, exception.LogContextOptions{}))
        }
    }()

    NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:refused", LeaderGateConfig{Ttl: bound.UnlimitedDuration()})
}

func TestNewLeaderGate_ALeaseLockerKeepsALeaseTtl(t *testing.T) {
    if gate := NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:lease", LeaderGateConfig{}); true == gate.settings.sessionMode {
        t.Fatalf("expected a lease")
    }
}

func TestLeaderGate_RunEndsWithTheRefusalOverALockerTheConstructorCannotRead(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewInMemoryLocker(InMemoryLockerConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    var campaignErrors atomic.Int32
    gate := NewLeaderGate(NewLazyLocker(serviceContainer), "worker:lazy", LeaderGateConfig{
        Ttl:           bound.UnlimitedDuration(),
        RetryInterval: bound.Of(time.Millisecond),
        OnCampaignError: func(runtimeInstance containercontract.Runtime, cause error) {
            campaignErrors.Add(1)
        },
    })

    runContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    runErr := gate.Run(runtime.NewRun(runContext, serviceContainer.NewScope()))
    if false == errors.Is(runErr, lockcontract.ErrUnlimitedTtlRefused) {
        t.Fatalf("expected Run to end with the refusal, got %v", runErr)
    }

    if nil != runContext.Err() {
        t.Fatalf("expected Run to end before the context did")
    }

    if 1 != campaignErrors.Load() {
        t.Fatalf("expected the refusal reported once through the hook, got %d", campaignErrors.Load())
    }
}

func TestLeaderGate_RunLogsTheRefusalOnceWithNoHookWired(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[lockcontract.Locker](
        serviceContainer,
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewInMemoryLocker(InMemoryLockerConfig{}), nil
        },
        containercontract.RegisterOptions{},
    )

    gate := NewLeaderGate(NewLazyLocker(serviceContainer), "worker:lazy-logged", LeaderGateConfig{Ttl: bound.UnlimitedDuration(), RetryInterval: bound.Of(time.Millisecond)})

    runContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    if runErr := gate.Run(runtimeInstance); false == errors.Is(runErr, lockcontract.ErrUnlimitedTtlRefused) {
        t.Fatalf("expected Run to end with the refusal, got %v", runErr)
    }

    if false == logger.hasMessageContaining("leader gate stopped: the lock refuses its unlimited ttl") {
        t.Fatalf("expected the refusal recorded")
    }
}

type selfReachingLockError struct{}

func (instance *selfReachingLockError) Error() string {
    return "self reaching"
}

func (instance *selfReachingLockError) Unwrap() error {
    return instance
}

func TestLeaderGate_AnAcquireErrorWhoseChainReachesItselfDoesNotHangTheCampaign(t *testing.T) {
    gate := NewLeaderGate(&failingAcquireLocker{err: &selfReachingLockError{}}, "worker:cycle", LeaderGateConfig{RetryInterval: bound.Of(time.Millisecond)})

    runContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    finished := make(chan error, 1)
    go func() {
        finished <- gate.Run(runtime.NewRun(runContext, serviceContainer.NewScope()))
    }()

    select {
    case runErr := <-finished:
        if nil != runErr {
            t.Fatalf("expected a clean shutdown, got %v", runErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("expected the campaign not to hang on the cyclic chain")
    }
}

var _ lockcontract.Locker = (*InMemoryLocker)(nil)

func TestNewExclusiveCommand_RefusesAnUnlimitedTtlOverALeaseLockerNamingTheField(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "an unlimited exclusive command ttl needs a session-style locker" != recoveredErr.Error() {
            t.Fatalf("expected the refusal, got %v", recoveredErr)
        }

        if "ExclusiveCommandConfig.Ttl" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the refusal to name the field, got %v", exception.LogContext(recoveredErr, exception.LogContextOptions{}))
        }
    }()

    NewExclusiveCommand(&recordingCommand{}, NewInMemoryLocker(InMemoryLockerConfig{}), ExclusiveCommandConfig{Ttl: bound.UnlimitedDuration()})
}

func TestLeaderGate_AnotherAcquireErrorStillBacksOffAndCampaignsAgain(t *testing.T) {
    var campaignErrors atomic.Int32
    gate := NewLeaderGate(&failingAcquireLocker{err: errors.New("store outage")}, "worker:outage", LeaderGateConfig{
        RetryInterval: bound.Of(time.Millisecond),
        OnCampaignError: func(runtimeInstance containercontract.Runtime, cause error) {
            campaignErrors.Add(1)
        },
    })

    runContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if runErr := gate.Run(runtime.NewRun(runContext, serviceContainer.NewScope())); nil != runErr {
        t.Fatalf("expected a clean shutdown, got %v", runErr)
    }

    if 2 > campaignErrors.Load() {
        t.Fatalf("expected the gate to campaign again after the outage, got %d", campaignErrors.Load())
    }
}

type failingAcquireLocker struct {
    err error
}

func (instance *failingAcquireLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    return &failingAcquireLock{err: instance.err}, nil
}

type failingAcquireLock struct {
    err error
}

func (instance *failingAcquireLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    return false, instance.err
}

func (instance *failingAcquireLock) Release(runtimeInstance containercontract.Runtime) error {
    return nil
}

func (instance *failingAcquireLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    return nil
}

func TestLeaderGate_RenewsTheLeaseWhileOnElectedWindsDownAfterAStop(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())

    locker := &countingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    var atStop atomic.Int64
    config := fastGateConfig(bound.Of(40 * time.Millisecond))
    config.RefreshInterval = bound.Of(10 * time.Millisecond)
    config.OnElected = func(termRuntime containercontract.Runtime) {
        cancel()
        <-termRuntime.Context().Done()
        atStop.Store(locker.refreshes())

        time.Sleep(120 * time.Millisecond)
    }

    if runErr := NewLeaderGate(locker, "worker:wind-down", config).Run(testRuntimeWithContext(runContext)); nil != runErr {
        t.Fatalf("expected a clean shutdown, got %v", runErr)
    }

    if renewed := locker.refreshes() - atStop.Load(); renewed < 2 {
        t.Fatalf("expected the lease renewed while OnElected wound down, got %d renewals after the stop", renewed)
    }
}

func TestLeaderGate_StopsRenewingPastTheStopRenewalLimit(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())

    locker := &countingRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    var pastLimit atomic.Int64
    var lostCause atomic.Value
    config := fastGateConfig(bound.Of(40 * time.Millisecond))
    config.RefreshInterval = bound.Of(10 * time.Millisecond)
    config.StopRenewalLimit = bound.Of(60 * time.Millisecond)
    config.OnElected = func(termRuntime containercontract.Runtime) {
        cancel()

        time.Sleep(100 * time.Millisecond)
        pastLimit.Store(locker.refreshes())
        time.Sleep(100 * time.Millisecond)
    }
    config.OnLost = func(_ containercontract.Runtime, cause error) {
        lostCause.Store(cause)
    }

    NewLeaderGate(locker, "worker:past-limit", config).Run(testRuntimeWithContext(runContext))

    if locker.refreshes() != pastLimit.Load() {
        t.Fatalf("expected no renewal past the limit, got %d after it", locker.refreshes()-pastLimit.Load())
    }

    if cause, _ := lostCause.Load().(error); nil == cause || false == strings.Contains(cause.Error(), "stop renewal limit") {
        t.Fatalf("expected OnLost to name the stop renewal limit, got %v", cause)
    }
}

func TestNewLeaderGate_RefusesAStopRenewalLimitBelowTheRefreshInterval(t *testing.T) {
    config := fastGateConfig(bound.Of(time.Second))
    config.RefreshInterval = bound.Of(200 * time.Millisecond)
    config.StopRenewalLimit = bound.Of(100 * time.Millisecond)

    if _, err := resolveLeaderGateConfig(config); nil == err || false == strings.Contains(err.Error(), "stop renewal limit") {
        t.Fatalf("expected the limit refused, got %v", err)
    }
}

func TestLeaderGate_ACleanWindDownUnderRenewalCallsNoOnLost(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())

    var lost atomic.Bool
    config := fastGateConfig(bound.Of(40 * time.Millisecond))
    config.RefreshInterval = bound.Of(10 * time.Millisecond)
    config.OnElected = func(termRuntime containercontract.Runtime) {
        cancel()
        <-termRuntime.Context().Done()
        time.Sleep(60 * time.Millisecond)
    }
    config.OnLost = func(containercontract.Runtime, error) {
        lost.Store(true)
    }

    NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:clean-wind-down", config).Run(testRuntimeWithContext(runContext))

    if true == lost.Load() {
        t.Fatal("expected a clean wind-down to call no OnLost")
    }
}

func TestLeaderGate_ALeaseLostDuringTheWindDownReachesOnLost(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())

    locker := &switchableRefreshLocker{inner: NewInMemoryLocker(InMemoryLockerConfig{})}

    var lostCause atomic.Value
    config := fastGateConfig(bound.Of(40 * time.Millisecond))
    config.RefreshInterval = bound.Of(10 * time.Millisecond)
    config.OnElected = func(termRuntime containercontract.Runtime) {
        locker.fail.Store(true)
        cancel()
        time.Sleep(150 * time.Millisecond)
    }
    config.OnLost = func(_ containercontract.Runtime, cause error) {
        lostCause.Store(cause)
    }

    NewLeaderGate(locker, "worker:lost-in-wind-down", config).Run(testRuntimeWithContext(runContext))

    if cause, _ := lostCause.Load().(error); nil == cause {
        t.Fatal("expected a lease lost while OnElected wound down to reach OnLost")
    }
}

func TestLeaderGate_AHookPanickingOnItsStoppedContextCallsNoOnLost(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())

    var lost atomic.Bool
    config := fastGateConfig(bound.Of(time.Minute))
    config.OnElected = func(termRuntime containercontract.Runtime) {
        cancel()
        <-termRuntime.Context().Done()
        panic("cleanup failed")
    }
    config.OnLost = func(containercontract.Runtime, error) {
        lost.Store(true)
    }

    NewLeaderGate(NewInMemoryLocker(InMemoryLockerConfig{}), "worker:panic-on-stop", config).Run(testRuntimeWithContext(runContext))

    if true == lost.Load() {
        t.Fatal("expected a hook panicking on its stopped context to call no OnLost")
    }
}

func TestLeaderGate_ReleasesItsTermOnAContextDetachedFromTheRunWithNoDeadlineOfItsOwn(t *testing.T) {
    locker := &releaseDeadlineLocker{remaining: make(chan time.Duration, 1), releaseErr: make(chan error, 1)}

    runContext, cancel := context.WithCancel(context.Background())
    gate := NewLeaderGate(locker, "worker:release-detached", fastGateConfig(bound.Of(time.Hour)))

    done := make(chan error, 1)
    go func() {
        done <- gate.Run(testRuntimeWithContext(runContext))
    }()

    waitUntil(t, 2*time.Second, gate.IsLeader, "expected the gate to lead")

    cancel()
    if runErr := <-done; nil != runErr {
        t.Fatalf("expected a clean shutdown, got: %v", runErr)
    }

    if remaining := <-locker.remaining; -1 != remaining {
        t.Fatalf("expected the term released under no deadline of the gate's, got %s left", remaining)
    }

    if contextErr := <-locker.releaseErr; nil != contextErr {
        t.Fatalf("expected the release context alive after the run was cancelled, got %v", contextErr)
    }
}

type decoratedLocker struct {
    lockcontract.Locker
}

func TestNewLeaderGate_RefusesAnUnlimitedTtlOverADecoratedLeaseLockerAtConstruction(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == errors.Is(recoveredErr, lockcontract.ErrUnlimitedTtlRefused) {
            t.Fatalf("expected the decorated lease locker's refusal at construction, got %v", recoveredErr)
        }
    }()

    NewLeaderGate(&decoratedLocker{Locker: NewInMemoryLocker(InMemoryLockerConfig{})}, "worker:decorated", LeaderGateConfig{Ttl: bound.UnlimitedDuration()})
}

func TestNewExclusiveCommand_RefusesAnUnlimitedTtlOverADecoratedLeaseLockerAtConstruction(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || false == errors.Is(recoveredErr, lockcontract.ErrUnlimitedTtlRefused) {
            t.Fatalf("expected the decorated lease locker's refusal at construction, got %v", recoveredErr)
        }
    }()

    NewExclusiveCommand(&recordingCommand{}, &decoratedLocker{Locker: NewInMemoryLocker(InMemoryLockerConfig{})}, ExclusiveCommandConfig{Ttl: bound.UnlimitedDuration()})
}

func TestNewLeaderGate_ALockerAnsweringNoLockIsRefusedAtConstruction(t *testing.T) {
    var recovered any
    func() {
        defer func() { recovered = recover() }()

        NewLeaderGate(&noLockLocker{}, "worker:none", LeaderGateConfig{})
    }()

    refusal, isMelodyErr := recovered.(*exception.Error)
    if false == isMelodyErr || false == namesNoLock(refusal) {
        t.Fatalf("expected a panic carrying the no-lock refusal, got %#v", recovered)
    }
}
