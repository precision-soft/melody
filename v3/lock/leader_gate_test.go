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

/* A gate that can never acquire — a redis locker built with a non-positive ttl fails closed on every Acquire — is otherwise indistinguishable from a healthy follower: it campaigns, backs off and elects nobody, silently, forever. */
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

/* A shutdown cancels the context the backend is called with, so the campaign in flight fails with that cancellation. Reporting it would hand every graceful stop an error indistinguishable from a store outage. */
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

/* A RetryInterval slower than the one-minute outage cap must not invert into faster-than-healthy retries: doubling the campaign backoff caps at the configured RetryInterval, never below it, so an outage never hammers the store more often than the healthy campaign cadence. */
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

/* An override RefreshInterval slower than half the lease ttl would let the lease lapse before the first renewal, so a second instance could acquire and both report leadership. NewLeaderGateWithOptions must clamp such an override down to the safe derived cadence (ttl/2). */
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

/* hangingRefreshLocker acquires and releases through the real locker but never answers a renewal: the store took the call and went quiet, the way a wedged connection or an unresponsive replica does. The call comes back only when the context handed to it is cancelled, which is precisely the deadline a renewal must carry. */
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

/* A renewal that takes longer than the lease is not the same failure the cadence clamp defends against: nothing lets the lease lapse in the schedule, the call itself simply never comes back. With leadership derived from "did the last renewal return an error", there is no error to derive it from, so the holder keeps claiming the lock while the lease runs out underneath it and a second instance acquires — two leaders, both certain. */
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

    gate := NewLeaderGateWithOptions(failing, "worker:hook-unwind", time.Minute, LeaderGateOptions{
        RetryInterval:   5 * time.Millisecond,
        RefreshInterval: 5 * time.Millisecond,
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

/* The budget of one renewal is a deadline on the CALL, not a verdict on the lease, so what it has to satisfy is that it never outlives the cadence it sits inside: an attempt that outlived it would still be in flight against the same lock when its successor started. The previous invariant — a budget well below the lease — was both wrong and untested where it mattered: it read the budget as the demotion signal, and it was sampled only at ttls of 100ms and up, where the floor never engages. Below two milliseconds the floor engaged on the cadence and on the budget independently and produced a budget LARGER than the cadence, which the old assertion never saw. */
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

/* countedFailureLocker fails the first failureCount renewals and answers every one after them, which is the shape of a store that dropped a connection and came back. */
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

/* A store that drops a connection and reconnects must not cost a term. The lease the gate last wrote is the store's own promise that nobody else gets this lock until it lapses — which is exactly why the cadence is half the lease — so a renewal lost while the lease runs has cost nothing, and the one behind it lands. Leaving on the first failure turned an eight-second failover into a cancelled term, a re-election, and leader work restarted from the beginning for a lock that was never in danger. */
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

/* the threshold is a knob, and turning it off has to leave the lease clock as the only signal — that is what a deployment asks for when it would rather run out the lease than give up a term early. */
func TestLeaderGate_ANegativeThresholdLeavesOnlyTheLeaseClock(t *testing.T) {
    gate := NewLeaderGateWithOptions(
        NewInMemoryLocker(clock.NewSystemClock()),
        "worker:lease-only",
        time.Minute,
        LeaderGateOptions{MaxConsecutiveRefreshFailures: -1},
    )

    if false == gate.refreshFailureEndsTheTerm(1000) {
        /* the lease is unset outside a term, so the lease clock alone already says the term is over; what matters is that the threshold did not decide it */
        t.Fatal("expected the lease clock to answer on its own")
    }

    gate.leaseExpiry.Store(time.Now().Add(time.Minute).UnixNano())

    if true == gate.refreshFailureEndsTheTerm(1000) {
        t.Fatal("a negative threshold must be off: a thousand failures may not end a term whose lease has a minute left")
    }
}

/* the default has to be reachable only where it is meant to be. At the documented cadence of half the ttl, three renewals already outlast the lease, so the lease clock decides and the threshold changes nothing for a gate that did not ask for a denser cadence. */
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

/* alternatingRefreshLocker fails every other renewal: losses that never land back to back, the shape of a lossy link rather than a store that has gone. */
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

/* The threshold counts failures that are CONSECUTIVE, so a renewal that lands has to clear the count. Without the reset the counter only ever climbs: a lossy link that drops one renewal in two — every one of them survived by the next — still reaches three eventually and ends a term that was never lost, which is the flapping the threshold exists to prevent rather than cause. Over the window below the gate accumulates far more than three individual failures and none of them are adjacent. */
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

/* In session mode there is no lease and no lease clock: the lock lives as long as the backend session does. Before the guard, enterTerm dated a "lease" from the acquire instant with a non-positive ttl — a deadline already in the past — so the FIRST failed liveness probe of every term found it beyond recovery and demoted immediately, overriding the documented three-failure tolerance the option promises. */
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

    /* the panic is recovered and recorded; the gate keeps its term — a hook failure is not a lost lease — and above all the process survives, where the unshielded form killed it with the lock held for the rest of its ttl on every peer */
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

    /* the shutdown released the lock: a fresh campaign must win it immediately */
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

    /* without the record a store outage and a permanent misconfiguration both look exactly like a deployment that quietly elects no leader and does no work */
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

    /* a panicking backend Refresh unwinds a bare goroutine with no recover of the caller's; recovered, it is the same demotion signal a returned error is — the process survives and the gate re-campaigns */
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

    /* the refresh cadence is left at its default — half a one-minute ttl — so no renewal can land before the assertion: what answers inside OnElected is the lease enterTerm dated from the acquire, the only lease that exists in the window between election and the first renewal. A gate that fails to store it reports a leader the fleet cannot see. */
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
