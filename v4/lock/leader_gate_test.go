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
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type switchableRefreshLocker struct {
    inner lockcontract.Locker
    fail  atomic.Bool
}

func (instance *switchableRefreshLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *switchableRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    if true == instance.locker.fail.Load() {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
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
    locker := NewInMemoryLocker(clock.NewSystemClock())

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
    locker := NewInMemoryLocker(clock.NewSystemClock())

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

    gate := NewLeaderGate(failing, "worker:lease", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
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

    /* the one-hour ttl must not matter: shutdown releases the lock immediately */
    contender := locker.CreateLock("worker:release", bound.Of(time.Minute))
    acquired, _ := contender.Acquire(testRuntime())
    if false == acquired {
        t.Fatalf("expected the lock to be free right after shutdown")
    }
}

/* A gate that can never acquire — a redis locker handed an unlimited ttl fails closed on every Acquire — is otherwise indistinguishable from a healthy follower: it campaigns, backs off and elects nobody, silently, forever. */
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

/* A shutdown cancels the context the backend is called with, so the campaign in flight fails with that cancellation. Reporting it would hand every graceful stop an error indistinguishable from a store outage. */
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

/* A RetryInterval slower than the one-minute outage cap must not invert into faster-than-healthy retries: doubling the campaign backoff caps at the configured RetryInterval, never below it, so an outage never hammers the store more often than the healthy campaign cadence. */
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

    /* a RetryInterval at or under the cap still caps at the cap, never runs away */
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

        NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:refresh-ceiling", LeaderGateConfig{
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

    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:refresh-ceiling", LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RefreshInterval: bound.Of(ttl / 2),
    })
    if ttl/2 != gate.settings.refreshInterval {
        t.Fatalf("expected half the ttl to be admitted, got %v", gate.settings.refreshInterval)
    }
}

func TestLeaderGate_RenewsTheLeaseWhileTheElectedHookRuns(t *testing.T) {
    counting := &countingRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGate(counting, "worker:elected-hook", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
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

    gate := NewLeaderGate(failing, "worker:elected-cancel", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
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

func (instance *countingRefreshLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *countingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    instance.locker.count.Add(1)

    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* contextSensitiveAcquireLocker fails Acquire with the call's context error, as a real backend does once the context is cancelled. */
type contextSensitiveAcquireLocker struct {
    entered chan struct{}
}

func (instance *contextSensitiveAcquireLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *contextSensitiveAcquireLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    return nil
}

/* hangingRefreshLocker acquires and releases through the real locker but never answers a renewal: the store took the call and went quiet, the way a wedged connection or an unresponsive replica does. The call comes back only when the context handed to it is cancelled, which is precisely the deadline a renewal must carry. */
type hangingRefreshLocker struct {
    inner lockcontract.Locker
}

func (instance *hangingRefreshLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *hangingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    <-runtimeInstance.Context().Done()

    return exception.NewError("lock refresh never answered", nil, runtimeInstance.Context().Err())
}

/* A renewal that takes longer than the lease is not the same failure the refresh-interval ceiling refuses: nothing lets the lease lapse in the schedule, the call itself simply never comes back. With leadership derived from "did the last renewal return an error", there is no error to derive it from, so the holder keeps claiming the lock while the lease runs out underneath it and a second instance acquires — two leaders, both certain. */
func TestLeaderGate_ARenewalThatNeverAnswersNeverYieldsTwoLeaders(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())

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

/* IsLeader and the hooks are two signals for one fact, and LOCK.md tells callers to combine them, so the claim must fall the moment the lease is provably lost — not when the hook that was elected finally unwinds, which is a duration the gate does not control and a hook ignoring its cancelled context never reaches at all. */
func TestLeaderGate_LeadershipDropsWhileTheElectedHookIsStillRunning(t *testing.T) {
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
    failing.fail.Store(true)

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    hookEntered := make(chan struct{})
    releaseHook := make(chan struct{})

    gate := NewLeaderGate(failing, "worker:hook-unwind", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
            close(hookEntered)

            /* a hook that outlives the lease: the gate cannot campaign again while it runs, so leadership is whatever IsLeader says it is */
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

/* the budget of one renewal is a deadline on the call, so it must never outlive the cadence it sits inside, or an attempt would still be in flight when its successor starts; sampled below two milliseconds, where the floor engages */
func TestLeaderGate_TheRenewalBudgetNeverOutlivesTheCadenceItSitsInside(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

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

/* countedFailureLocker fails the first failureCount renewals and answers every one after them, which is the shape of a store that dropped a connection and came back. */
type countedFailureLocker struct {
    inner        lockcontract.Locker
    failureCount int64
    attempts     atomic.Int64
}

func (instance *countedFailureLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *countedFailureLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    if instance.locker.attempts.Add(1) <= instance.locker.failureCount {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* A store that drops a connection and reconnects must not cost a term. The lease the gate last wrote is the store's own promise that nobody else gets this lock until it lapses — which is exactly why the cadence is half the lease — so a renewal lost while the lease runs costs nothing, and the one behind it lands. Leaving on the first failure would turn an eight-second failover into a cancelled term, a re-election, and leader work restarted from the beginning for a lock that is not in danger. */
func TestLeaderGate_ASingleFailedRenewalDoesNotCostTheTerm(t *testing.T) {
    failing := &countedFailureLocker{inner: NewInMemoryLocker(clock.NewSystemClock()), failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:blip", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
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

/* the other half: a store that is simply gone must end the term rather than let leader work run out the whole lease. The lease clock cannot see this on its own here — the cadence is far denser than the lease, so the lease still has a minute left — which is precisely the gap the consecutive-failure threshold covers. */
func TestLeaderGate_ThresholdConsecutiveFailuresEndTheTermWhileTheLeaseIsStillValid(t *testing.T) {
    failing := &switchableRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}
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

/* the threshold is a knob, and turning it off has to leave the lease clock as the only signal — that is what a deployment asks for when it would rather run out the lease than give up a term early. */
func TestLeaderGate_AnUnlimitedThresholdLeavesOnlyTheLeaseClock(t *testing.T) {
    gate := NewLeaderGate(
        NewInMemoryLocker(clock.NewSystemClock()),
        "worker:lease-only",
        LeaderGateConfig{Ttl: bound.Of(time.Minute), MaxConsecutiveRefreshFailures: bound.UnlimitedCount()},
    )

    if false == gate.refreshFailureEndsTheTerm(1000) {
        /* the lease is unset outside a term, so the lease clock alone already says the term is over; what matters is that the threshold did not decide it */
        t.Fatal("expected the lease clock to answer on its own")
    }

    gate.leaseExpiry.Store(time.Now().Add(time.Minute).UnixNano())

    if true == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("an unlimited threshold must be off: a thousand failures may not end a term whose lease has a minute left")
    }
}

/* the default has to be reachable only where it is meant to be. At the documented cadence of half the ttl, three renewals already outlast the lease, so the lease clock decides and the threshold changes nothing for a gate that did not ask for a denser cadence. */
func TestLeaderGate_TheDefaultThresholdIsUnreachableAtTheDefaultCadence(t *testing.T) {
    ttl := time.Minute

    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:default", LeaderGateConfig{Ttl: bound.Of(ttl)})

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

/* alternatingRefreshLocker fails every other renewal: losses that never land back to back, the shape of a lossy link rather than a store that has gone. */
type alternatingRefreshLocker struct {
    inner    lockcontract.Locker
    attempts atomic.Int64
}

func (instance *alternatingRefreshLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
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

func (instance *alternatingRefreshLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    if 0 == instance.locker.attempts.Add(1)%2 {
        return exception.NewError("lease lost", nil, nil)
    }

    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* The threshold counts failures that are CONSECUTIVE, so a renewal that lands has to clear the count. Without the reset the counter only ever climbs: a lossy link that drops one renewal in two — every one of them survived by the next — still reaches three eventually and ends a term that was never lost, which is the flapping the threshold exists to prevent rather than cause. Over the window below the gate accumulates far more than three individual failures and none of them are adjacent. */
func TestLeaderGate_ScatteredFailuresNeverAccumulateIntoADemotion(t *testing.T) {
    failing := &alternatingRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:lossy", LeaderGateConfig{
        Ttl:                           bound.Of(time.Minute),
        RetryInterval:                 bound.Of(5 * time.Millisecond),
        RefreshInterval:               bound.Of(5 * time.Millisecond),
        MaxConsecutiveRefreshFailures: bound.Of(3),
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

/* In session mode there is no lease and no lease clock: the lock lives as long as the backend session does. Before the guard, enterTerm dated a "lease" from the acquire instant with no ttl — a deadline already in the past — so the FIRST failed liveness probe of every term found it beyond recovery and demoted immediately, overriding the documented three-failure tolerance the option promises. */
func TestLeaderGate_SessionModeToleratesTransientProbeFailures(t *testing.T) {
    failing := &countedFailureLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}, failureCount: 1}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    var lostCount atomic.Int64
    elected := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:session-blip", LeaderGateConfig{
        Ttl:             bound.UnlimitedDuration(),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(20 * time.Millisecond),
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
    failing := &countedFailureLocker{inner: &sessionStyleLocker{inner: NewInMemoryLocker(clock.NewSystemClock())}, failureCount: 1 << 30}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    lost := make(chan struct{}, 16)

    gate := NewLeaderGate(failing, "worker:session-gone", LeaderGateConfig{
        Ttl:             bound.UnlimitedDuration(),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lost <- struct{}{}
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    /* with no lease clock in session mode, the consecutive-failure threshold is the only demotion signal — a store that is plainly gone must still end the term */
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

    gate := NewLeaderGate(locker, "worker:hook-panics", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime runtimecontract.Runtime) {
            panic(exception.NewError("hook exploded", nil, nil))
        },
    })

    runDone := make(chan struct{})
    go func() {
        _ = gate.Run(runtimeInstance)
        close(runDone)
    }()

    /* the panic is recovered and recorded, and above all the process survives, where the unshielded form killed it with the lock held for the rest of its ttl on every peer; the term it was leading ends with it, which the sibling tests below pin */
    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate hook panicked") }, "expected the hook panic to be logged")

    waitUntil(t, 2*time.Second, func() bool { return false == gate.IsLeader() }, "expected the term to end after the recovered hook panic")

    cancel()

    select {
    case <-runDone:
    case <-time.After(2 * time.Second):
        t.Fatalf("the gate did not shut down cleanly after the recovered panic")
    }

    /* the shutdown released the lock: a fresh campaign must win it immediately */
    acquired, acquireErr := locker.CreateLock("worker:hook-panics", bound.Of(time.Minute)).Acquire(testRuntimeWithContext(context.Background()))
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

    /* without the record a store outage and a permanent misconfiguration both look exactly like a deployment that quietly elects no leader and does no work */
    waitUntil(t, 2*time.Second, func() bool { return logger.hasMessageContaining("leader gate campaign failed") }, "expected the failed campaign to be logged")
}

func TestLeaderGate_CampaignErrorHookReplacesTheDefaultRecord(t *testing.T) {
    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    hookCalls := make(chan error, 16)

    options := fastGateConfig(bound.Of(time.Minute))
    options.OnCampaignError = func(hookRuntime runtimecontract.Runtime, cause error) {
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
        OnLost: func(runtimeInstance runtimecontract.Runtime, cause error) {
            lost <- cause
        },
    })

    go gate.Run(testRuntimeWithContext(runContext))

    /* a panicking backend Refresh unwinds a bare goroutine with no recover of the caller's; recovered, it is the same demotion signal a returned error is — the process survives and the gate re-campaigns */
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

    /* the refresh cadence is left at its default — half a one-minute ttl — so no renewal can land before the assertion: what answers inside OnElected is the lease enterTerm dated from the acquire, the only lease that exists in the window between election and the first renewal. A gate that fails to store it reports a leader the fleet cannot see. */
    var gate *LeaderGate
    gate = NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:first-window", LeaderGateConfig{
        Ttl:           bound.Of(time.Minute),
        RetryInterval: bound.Of(5 * time.Millisecond),
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

/* the shield keeps the process alive, but not the term: a gate parked on a term whose work died would renew a lease under nothing and answer IsLeader */
func TestLeaderGate_APanickingOnElectedEndsTheTermAndReleasesTheLock(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, _ := runtimeWithRecordingLogger(runContext)

    lostCauses := make(chan error, 4)
    gate := NewLeaderGate(locker, "worker:hook-panics-term", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(time.Second),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime runtimecontract.Runtime) {
            panic(exception.NewError("hook exploded", nil, nil))
        },
        OnLost: func(lostRuntime runtimecontract.Runtime, cause error) {
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

    if "OnElected" != exception.LogContext(lostCause, exception.LogContextConfig{})["hook"] {
        t.Fatalf("expected the lost cause to name the hook, got %v", exception.LogContext(lostCause, exception.LogContextConfig{}))
    }

    if true == gate.IsLeader() {
        t.Fatalf("expected the gate to have left its term")
    }

    /* the lock is free between the ended term and the next campaign, a whole RetryInterval away: another replica can take it now */
    acquired, acquireErr := locker.CreateLock("worker:hook-panics-term", bound.Of(time.Minute)).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock to be released with the ended term, got acquired=%v err=%v", acquired, acquireErr)
    }
}

func TestLeaderGate_APanickingOnElectedIsCampaignedAgainAfterRetryInterval(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, _ := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:hook-panics-again", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime runtimecontract.Runtime) {
            elections.Add(1)
            panic("hook exploded")
        },
    })

    go func() { _ = gate.Run(runtimeInstance) }()

    waitUntil(t, 2*time.Second, func() bool { return 2 <= elections.Load() }, "expected the gate to campaign again after the panicking term ended")
}

/* OnLost runs after the term has already ended and OnCampaignError outside any term, so their panics have no term to end: the shield journals them and the loop goes on — the mutant that would demote on every hook is told apart here */
func TestLeaderGate_APanickingOnLostDoesNotStopTheCampaigns(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:lost-panics", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(5 * time.Millisecond),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime runtimecontract.Runtime) {
            elections.Add(1)
            panic("elected exploded")
        },
        OnLost: func(lostRuntime runtimecontract.Runtime, cause error) {
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
    ctx       context.Context
    container containercontract.Container
}

func (instance *typedNilScopeRuntime) Context() context.Context       { return instance.ctx }
func (instance *typedNilScopeRuntime) Scope() containercontract.Scope { return (*nilableTestScope)(nil) }
func (instance *typedNilScopeRuntime) Container() containercontract.Container {
    return instance.container
}

/* the runtime package's resolution doors tolerate a typed-nil scope, so such a runtime reaches the gate; it is refused before campaigning, since runtime.New would refuse it inside the deferred release, a second panic with the lock held */
func TestLeaderGateRun_RefusesATypedNilScopeBeforeCampaigning(t *testing.T) {
    locker := NewInMemoryLocker(clock.NewSystemClock())
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    gate := NewLeaderGate(locker, "worker:typed-nil-scope", LeaderGateConfig{Ttl: bound.Of(time.Minute)})

    testhelper.AssertPanicsWithError(t, func() {
        _ = gate.Run(&typedNilScopeRuntime{ctx: context.Background(), container: serviceContainer})
    }, "leader gate runtime scope is nil")

    acquired, acquireErr := locker.CreateLock("worker:typed-nil-scope", bound.Of(time.Minute)).Acquire(testRuntimeWithContext(context.Background()))
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected no lock to be held by the refused run, got acquired=%v err=%v", acquired, acquireErr)
    }
}

/* measures how long before a second gate is elected the first gate's hook, still working, is told to stop; the first gate renews through firstLocker, over the same store the second campaigns on */
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

    /* the hook keeps working after it is told to stop, so the lock is not released and the second gate can only take it once the lease lapses */
    first := NewLeaderGate(firstLocker, "worker:handover", LeaderGateConfig{
        Ttl:             bound.Of(ttl),
        RetryInterval:   bound.Of(time.Minute),
        RefreshInterval: bound.Of(ttl / 2),
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
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
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
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

/* the renewal issued at the cadence never answers; bounded to the lease's demotion instant it is cut there, so an elected hook still working is told to stop a quarter of the ttl before the lease lapses and a second gate takes the lock, rather than at the lapse itself */
func TestLeaderGate_AHungRenewalStopsTheElectedHookBeforeASecondGateIsElected(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())
    ttl := 400 * time.Millisecond

    if margin := handoverMarginUnder(t, innerLocker, &hangingRefreshLocker{inner: innerLocker}, ttl); margin < ttl/8 {
        t.Fatalf("expected the first hook told to stop at least an eighth of the ttl before the second gate was elected, stopped %v before", margin)
    }
}

/* every renewal fails at once; the one retry halfway to the demotion instant fails too, and the timer ends the term there instead of the next tick ending it at the lapse */
func TestLeaderGate_AFailingRenewalStopsTheElectedHookBeforeASecondGateIsElected(t *testing.T) {
    innerLocker := NewInMemoryLocker(clock.NewSystemClock())
    ttl := 400 * time.Millisecond

    failing := &switchableRefreshLocker{inner: innerLocker}
    failing.fail.Store(true)

    if margin := handoverMarginUnder(t, innerLocker, failing, ttl); margin < ttl/8 {
        t.Fatalf("expected the first hook told to stop at least an eighth of the ttl before the second gate was elected, stopped %v before", margin)
    }
}

/* parkingReleaseLocker holds every Release until the test lets it go, so the instant between leaving the term and the lock reaching the store can be observed */
type parkingReleaseLocker struct {
    inner          lockcontract.Locker
    releaseEntered chan struct{}
    releaseProceed chan struct{}
    enteredOnce    sync.Once
}

func (instance *parkingReleaseLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
    return &parkingReleaseLock{locker: instance, inner: instance.inner.CreateLock(name, ttl)}
}

type parkingReleaseLock struct {
    locker *parkingReleaseLocker
    inner  lockcontract.Lock
}

func (instance *parkingReleaseLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.inner.Acquire(runtimeInstance)
}

func (instance *parkingReleaseLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.locker.enteredOnce.Do(func() {
        close(instance.locker.releaseEntered)
    })
    <-instance.locker.releaseProceed

    return instance.inner.Release(runtimeInstance)
}

func (instance *parkingReleaseLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* the term is left before the lock is released: while the release is still on its way to the store the lease is valid, and a gate that still claimed leadership then would answer IsLeader for a lock it is handing back */
func TestLeaderGate_LeavesTheTermBeforeTheReleaseReachesTheStore(t *testing.T) {
    locker := &parkingReleaseLocker{
        inner:          NewInMemoryLocker(clock.NewSystemClock()),
        releaseEntered: make(chan struct{}),
        releaseProceed: make(chan struct{}),
    }

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    elected := make(chan struct{})
    gate := NewLeaderGate(locker, "worker:leave-then-release", LeaderGateConfig{
        Ttl:           bound.Of(time.Minute),
        RetryInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
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

/* the renewal ignores its context and answers success only after the demotion instant: the elected hook is still told to stop at the instant, before the lease it was elected under lapses */
func TestLeaderGate_ARenewalIgnoringItsContextStopsTheElectedHookAtTheDemotionInstant(t *testing.T) {
    ttl := 400 * time.Millisecond
    locker := &contextIgnoringRefreshLocker{inner: NewInMemoryLocker(clock.NewSystemClock()), delay: 180 * time.Millisecond}

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    stoppedAfter := make(chan time.Duration, 1)
    gate := NewLeaderGate(locker, "worker:context-ignoring", LeaderGateConfig{
        Ttl:           bound.Of(ttl),
        RetryInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(runtimeInstance runtimecontract.Runtime) {
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
    locker := NewInMemoryLocker(clock.NewSystemClock())

    runContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance, logger := runtimeWithRecordingLogger(runContext)

    var elections atomic.Int32
    gate := NewLeaderGate(locker, "worker:hook-panics-once", LeaderGateConfig{
        Ttl:             bound.Of(time.Minute),
        RetryInterval:   bound.Of(time.Hour),
        RefreshInterval: bound.Of(5 * time.Millisecond),
        OnElected: func(electedRuntime runtimecontract.Runtime) {
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
    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:zero-config", LeaderGateConfig{})

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
    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:session-config", LeaderGateConfig{Ttl: bound.UnlimitedDuration()})

    if false == gate.settings.sessionMode || false == gate.lockTtl().IsUnlimited() {
        t.Fatalf("expected session mode and an unlimited ttl handed to the locker, got %v and %s", gate.settings.sessionMode, gate.lockTtl())
    }

    if time.Second != gate.settings.retryInterval || defaultSessionProbeInterval != gate.settings.refreshInterval {
        t.Fatalf("expected a 1s retry and the 15s probe, got retry %s refresh %s", gate.settings.retryInterval, gate.settings.refreshInterval)
    }
}

func TestNewLeaderGate_AShortLeaseRetriesNoFasterThanOnceASecond(t *testing.T) {
    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:short-lease", LeaderGateConfig{Ttl: bound.Of(time.Second)})

    if time.Second != gate.settings.retryInterval || 500*time.Millisecond != gate.settings.refreshInterval {
        t.Fatalf("expected a 1s retry floor and a 500ms refresh, got retry %s refresh %s", gate.settings.retryInterval, gate.settings.refreshInterval)
    }
}

func TestNewLeaderGate_TheSetFieldsReachTheGate(t *testing.T) {
    gate := NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:set-config", LeaderGateConfig{
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

                NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:refused", testCase.config)
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

        NewLeaderGate(NewInMemoryLocker(clock.NewSystemClock()), "worker:no-signal", LeaderGateConfig{
            Ttl:                           bound.UnlimitedDuration(),
            MaxConsecutiveRefreshFailures: bound.UnlimitedCount(),
        })
    }()

    refusal, isMelodyErr := recovered.(*exception.Error)
    if false == isMelodyErr || "LeaderGateConfig.MaxConsecutiveRefreshFailures" != refusal.Context()["field"] || "LeaderGateConfig.Ttl" != refusal.Context()["besideField"] {
        t.Fatalf("expected a panic naming both fields, got %#v", recovered)
    }
}
