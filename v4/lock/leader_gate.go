package lock

import (
    "context"
    "runtime/debug"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* leaderRetryFloor floors the derived RetryInterval, so a short lease does not campaign many times a second while another instance leads. */
const leaderRetryFloor = 1 * time.Second

var leaderGateReleaseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "LeaderGateConfig.ReleaseTimeout",
    Default: exclusiveReleaseTimeoutRule.Default,
    Minimum: exclusiveReleaseTimeoutRule.Minimum,
}

/* LeaderGateConfig is what a gate declares beyond its locker and its lock name; every zero value is the default, the intervals derived from the ttl. */
type LeaderGateConfig struct {
    /* Ttl is the lease a term holds and renews: 30s by default, at least 2ms, unlimited selecting a session-style lock (MySQL GET_LOCK, PostgreSQL advisory), which has no lease to extend and is probed instead. */
    Ttl bound.Duration

    /* RetryInterval is the pause between failed campaigns while another instance leads: half the ttl, at least 1s, by default, 1s under a session-style lock; at least 1ms when set; unlimited refused. */
    RetryInterval bound.Duration

    /* RefreshInterval is the renewal cadence while leading: half the ttl by default, 15s under a session-style lock; at least 1ms, and at most half the ttl, since a slower cadence lets the lease lapse before its first renewal; unlimited refused. */
    RefreshInterval bound.Duration

    /* MaxConsecutiveRefreshFailures is how many renewals in a row may fail before the gate leaves its term, whatever the lease still says: 3 by default, at least 1, unlimited leaving the lease clock as the only signal. A session-style lock has no lease clock, so an unlimited threshold beside an unlimited ttl is refused. The threshold bites only where the cadence is much denser than the lease: at the default cadence of half the ttl the lease clock decides first. */
    MaxConsecutiveRefreshFailures bound.Count

    /* MaxCampaignBackoff caps the doubling pause between campaigns that could not ask the store who leads, so a persistent outage neither tight-loops the gate nor pushes re-election out indefinitely: 1m by default, never below RetryInterval, at least 1ms, unlimited refused. */
    MaxCampaignBackoff bound.Duration

    /* ReleaseTimeout bounds the release at the end of a term, detached from the term's context: 5s by default, at least 1ms, unlimited refused. */
    ReleaseTimeout bound.Duration

    /* OnElected runs on the Run goroutine right after the gate becomes leader, with the lease renewal already running. Its runtime carries a context cancelled when the lease is lost, and while it blocks the gate cannot campaign again. A panic out of it ends the term: the lock is released, OnLost receives the panic as the cause, and the gate campaigns again after RetryInterval. */
    OnElected func(runtimeInstance runtimecontract.Runtime)

    /* OnLost runs on the Run goroutine right after leadership is lost — to a failed renewal, whose error is the cause, or to a panic out of OnElected, whose recovered value reaches it as the cause of a "leader gate hook panicked" error; when both happen in one term the renewal error wins and the panic is journaled beside it. It does not run on a clean shutdown. Left nil, the gate logs the lost term through the runtime's logger instead; wiring the hook replaces that record. */
    OnLost func(runtimeInstance runtimecontract.Runtime, cause error)

    /* OnCampaignError runs on the Run goroutine for every campaign that could not ask the store who leads; the gate then backs off and campaigns again. Left nil, the gate logs each failed campaign, since an outage and a misconfiguration both look like a deployment that elects no leader. Wiring the hook replaces that record. */
    OnCampaignError func(runtimeInstance runtimecontract.Runtime, cause error)
}

var leaderGateTtlRule = bound.Rule[time.Duration]{
    Name:              "LeaderGateConfig.Ttl",
    Default:           lockcontract.LockTtlRule.Default,
    Minimum:           minimumLeaseTtl,
    UnlimitedAdmitted: true,
}

var leaderGateMaxConsecutiveRefreshFailuresRule = bound.Rule[int]{
    Name:              "LeaderGateConfig.MaxConsecutiveRefreshFailures",
    Default:           3,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var leaderGateMaxCampaignBackoffRule = bound.Rule[time.Duration]{
    Name:    "LeaderGateConfig.MaxCampaignBackoff",
    Default: 1 * time.Minute,
    Minimum: bound.TimeoutMinimum,
}

type leaderGateSettings struct {
    releaseTimeout                time.Duration
    ttl                           time.Duration
    sessionMode                   bool
    retryInterval                 time.Duration
    refreshInterval               time.Duration
    maxConsecutiveRefreshFailures int
    refreshFailuresUnlimited      bool
    maxCampaignBackoff            time.Duration
    onElected                     func(runtimeInstance runtimecontract.Runtime)
    onLost                        func(runtimeInstance runtimecontract.Runtime, cause error)
    onCampaignError               func(runtimeInstance runtimecontract.Runtime, cause error)
}

/* NewLeaderGate builds a gate campaigning for the named lock. The configuration is resolved here, and a refusal panics naming the field. */
func NewLeaderGate(locker lockcontract.Locker, name string, config LeaderGateConfig) *LeaderGate {
    if true == internal.IsNilInterface(locker) {
        exception.Panic(exception.NewError("leader gate locker is nil", nil, nil))
    }

    if "" == name {
        exception.Panic(exception.NewError("leader gate lock name is empty", nil, nil))
    }

    settings, err := resolveLeaderGateConfig(config)
    if nil != err {
        exception.Panic(exception.FromError(err))
    }

    return &LeaderGate{
        locker:     locker,
        name:       name,
        settings:   settings,
        timeAnchor: time.Now(),
    }
}

func resolveLeaderGateConfig(config LeaderGateConfig) (leaderGateSettings, error) {
    ttl, ttlErr := bound.Resolve(config.Ttl, leaderGateTtlRule)
    if nil != ttlErr {
        return leaderGateSettings{}, ttlErr
    }

    derivedRetryInterval := leaderRetryFloor
    derivedRefreshInterval := defaultSessionProbeInterval
    if false == ttl.Unlimited {
        derivedRetryInterval = max(ttl.Value/2, leaderRetryFloor)
        derivedRefreshInterval = max(ttl.Value/2, minimumRefreshInterval)
    }

    retryInterval, retryIntervalErr := bound.Resolve(
        config.RetryInterval,
        bound.Rule[time.Duration]{Name: "LeaderGateConfig.RetryInterval", Default: derivedRetryInterval, Minimum: bound.TimeoutMinimum},
    )
    if nil != retryIntervalErr {
        return leaderGateSettings{}, retryIntervalErr
    }

    refreshInterval, refreshIntervalErr := bound.Resolve(
        config.RefreshInterval,
        bound.Rule[time.Duration]{Name: "LeaderGateConfig.RefreshInterval", Default: derivedRefreshInterval, Minimum: minimumRefreshInterval},
    )
    if nil != refreshIntervalErr {
        return leaderGateSettings{}, refreshIntervalErr
    }

    /* a cadence slower than half the lease lets the lease lapse before the first renewal, so a second instance could lead too; the derived cadence is the ceiling, floored like it */
    if false == ttl.Unlimited && refreshInterval.Value > derivedRefreshInterval {
        return leaderGateSettings{}, exception.NewError(
            "the refresh interval exceeds half the lease",
            exceptioncontract.Context{
                "field":   "LeaderGateConfig.RefreshInterval",
                "value":   refreshInterval.Value.String(),
                "maximum": derivedRefreshInterval.String(),
            },
            nil,
        )
    }

    maxConsecutiveRefreshFailures, maxConsecutiveRefreshFailuresErr := bound.Resolve(config.MaxConsecutiveRefreshFailures, leaderGateMaxConsecutiveRefreshFailuresRule)
    if nil != maxConsecutiveRefreshFailuresErr {
        return leaderGateSettings{}, maxConsecutiveRefreshFailuresErr
    }

    if true == ttl.Unlimited && true == maxConsecutiveRefreshFailures.Unlimited {
        return leaderGateSettings{}, exception.NewError(
            "an unlimited refresh failure threshold leaves a session-style lock no signal to end a term",
            exceptioncontract.Context{
                "field":       leaderGateMaxConsecutiveRefreshFailuresRule.Name,
                "besideField": leaderGateTtlRule.Name,
            },
            nil,
        )
    }

    maxCampaignBackoff, maxCampaignBackoffErr := bound.Resolve(config.MaxCampaignBackoff, leaderGateMaxCampaignBackoffRule)
    if nil != maxCampaignBackoffErr {
        return leaderGateSettings{}, maxCampaignBackoffErr
    }

    releaseTimeout, releaseTimeoutErr := bound.Resolve(config.ReleaseTimeout, leaderGateReleaseTimeoutRule)
    if nil != releaseTimeoutErr {
        return leaderGateSettings{}, releaseTimeoutErr
    }

    return leaderGateSettings{
        releaseTimeout:                releaseTimeout.Value,
        ttl:                           ttl.Value,
        sessionMode:                   ttl.Unlimited,
        retryInterval:                 retryInterval.Value,
        refreshInterval:               refreshInterval.Value,
        maxConsecutiveRefreshFailures: maxConsecutiveRefreshFailures.Value,
        refreshFailuresUnlimited:      maxConsecutiveRefreshFailures.Unlimited,
        maxCampaignBackoff:            maxCampaignBackoff.Value,
        onElected:                     config.OnElected,
        onLost:                        config.OnLost,
        onCampaignError:               config.OnCampaignError,
    }, nil
}

/* LeaderGate is the become-leader, renew-periodically, release-on-shutdown pattern over any lock backend: Run campaigns for the named lock, holds and renews it while leading, demotes itself and re-campaigns when a renewal fails, and releases the lock on shutdown. Wrap the work itself in a check on IsLeader, or hook OnElected/OnLost. An unlimited ttl selects session-style locks (MySQL GET_LOCK, PostgreSQL advisory): there is no lease to extend, so the renewal is a liveness probe. */
type LeaderGate struct {
    locker   lockcontract.Locker
    name     string
    settings leaderGateSettings

    /* the lease deadline is stored as an offset from this monotonic instant, so a backwards wall-clock step cannot extend perceived leadership past the real lease */
    timeAnchor time.Time

    /* inTerm marks a term entered and not yet left; leaseExpiry is the instant the held lease lapses, as nanoseconds since timeAnchor, and zero outside a term. The flag alone cannot expire, and the deadline alone cannot tell an untaken lock from a lease still running out. */
    inTerm      atomic.Bool
    leaseExpiry atomic.Int64
}

func (instance *LeaderGate) leaseExpiryOffset(issuedAt time.Time, ttl time.Duration) int64 {
    return int64(issuedAt.Sub(instance.timeAnchor) + ttl)
}

/* IsLeader answers from the lease: the gate leads while it is inside a term and the lease it took or last renewed is still in the future, so a renewal that never answers cannot keep it leading. In session mode (an unlimited ttl) there is no lease, and the term is the whole answer. */
func (instance *LeaderGate) IsLeader() bool {
    if false == instance.inTerm.Load() {
        return false
    }

    if true == instance.settings.sessionMode {
        return true
    }

    expiry := instance.leaseExpiry.Load()

    return 0 != expiry && int64(time.Since(instance.timeAnchor)) < expiry
}

/* enterTerm publishes the lease before the term, so no reader sees leadership backed by an ended term's deadline. The lease is dated from the instant the acquire was issued, since the store started it somewhere inside the call; in session mode the offset stays zero. */
func (instance *LeaderGate) enterTerm(acquireIssuedAt time.Time) {
    if false == instance.settings.sessionMode {
        instance.leaseExpiry.Store(instance.leaseExpiryOffset(acquireIssuedAt, instance.settings.ttl))
    }
    instance.inTerm.Store(true)
}

/* lockTtl is the ttl each campaign hands the locker: the lease, or unlimited in session mode. */
func (instance *LeaderGate) lockTtl() bound.Duration {
    if true == instance.settings.sessionMode {
        return bound.UnlimitedDuration()
    }

    return bound.Of(instance.settings.ttl)
}

func (instance *LeaderGate) leaveTerm() {
    instance.inTerm.Store(false)
    instance.leaseExpiry.Store(0)
}

/* Run blocks until the runtime context is cancelled and always returns nil on a clean shutdown; start it with `go gate.Run(runtimeInstance)` for a long-running worker. Acquire errors (a store outage) never abort it — they back off doubling, capped at MaxCampaignBackoff or at RetryInterval when that is longer, and campaigning resumes. Because they never abort it, they are also never returned: each one is logged through the runtime's logger unless OnCampaignError is wired, in which case the hook owns the record. */
func (instance *LeaderGate) Run(runtimeInstance runtimecontract.Runtime) error {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(exception.NewError("leader gate runtime is nil", nil, nil))
    }

    refuseARuntimeThatCannotBeRewrapped(runtimeInstance, "leader gate")

    runContext := runtimeInstance.Context()
    campaignBackoff := instance.settings.retryInterval

    for {
        if nil != runContext.Err() {
            return nil
        }

        /* a fresh lock per campaign: every CreateLock mints a new fencing token, and a lock reused after losing it would alias the tokens of the holder that took it over */
        lock := instance.locker.CreateLock(instance.name, instance.lockTtl())

        acquireIssuedAt := time.Now()
        acquired, acquireErr := lock.Acquire(runtimeInstance)
        if nil != acquireErr {
            /* a shutdown cancels the context the backend is called with, so a campaign failing with the cancellation is the stop itself, not a store outage */
            if nil != runContext.Err() {
                return nil
            }

            instance.reportCampaignError(runtimeInstance, acquireErr)

            if false == sleepUnlessDone(runContext, campaignBackoff) {
                return nil
            }

            campaignBackoff = nextCampaignBackoff(campaignBackoff, instance.settings.retryInterval, instance.settings.maxCampaignBackoff)

            continue
        }

        campaignBackoff = instance.settings.retryInterval

        if false == acquired {
            if false == sleepUnlessDone(runContext, instance.settings.retryInterval) {
                return nil
            }

            continue
        }

        lostCause := instance.holdTerm(runtimeInstance, lock, acquireIssuedAt)

        if nil != runContext.Err() {
            return nil
        }

        instance.reportLost(runtimeInstance, lostCause)

        if false == sleepUnlessDone(runContext, instance.settings.retryInterval) {
            return nil
        }
    }
}

/* holdTerm enters the term, leads it and leaves it with the lock released, through defers, so a panicking OnElected or backend still drops the claim and releases the lock. */
func (instance *LeaderGate) holdTerm(
    runtimeInstance runtimecontract.Runtime,
    lock lockcontract.Lock,
    acquireIssuedAt time.Time,
) error {
    instance.enterTerm(acquireIssuedAt)

    defer releaseDetached(runtimeInstance, lock, instance.name, instance.settings.releaseTimeout)
    defer instance.leaveTerm()

    return instance.lead(runtimeInstance, lock)
}

/* reportCampaignError hands a failed campaign to the OnCampaignError hook when one is wired, and records it itself otherwise; a wired hook replaces the record. */
func (instance *LeaderGate) reportCampaignError(runtimeInstance runtimecontract.Runtime, cause error) {
    if nil != instance.settings.onCampaignError {
        instance.runHookShielded(runtimeInstance, "OnCampaignError", func() {
            instance.settings.onCampaignError(runtimeInstance, cause)
        })

        return
    }

    instance.gateLogger(runtimeInstance).Warning(
        "leader gate campaign failed; backing off and campaigning again",
        exception.LogContext(cause, exception.LogContextConfig{Extra: exceptioncontract.Context{"name": instance.name}}),
    )
}

func (instance *LeaderGate) reportLost(runtimeInstance runtimecontract.Runtime, cause error) {
    if nil != instance.settings.onLost {
        instance.runHookShielded(runtimeInstance, "OnLost", func() {
            instance.settings.onLost(runtimeInstance, cause)
        })

        return
    }

    /* a hook's panic is journaled with its value and its stack where the shield recovered it, so the lost term names the hook alone instead of carrying them a second time */
    if hookName, isHookPanic := leaderGateHookPanicName(cause); true == isHookPanic {
        instance.gateLogger(runtimeInstance).Warning(
            "leader gate lost the term after a hook panicked; campaigning again",
            exceptioncontract.Context{"name": instance.name, "hook": hookName},
        )

        return
    }

    instance.gateLogger(runtimeInstance).Warning(
        "leader gate lost its term; campaigning again",
        exception.LogContext(cause, exception.LogContextConfig{Extra: exceptioncontract.Context{"name": instance.name}}),
    )
}

/* leaderGateHookPanicName answers the hook a cause is the shield's report of, the error runHookShielded answers for a panic */
func leaderGateHookPanicName(cause error) (string, bool) {
    hookErr, isMelodyErr := cause.(*exception.Error)
    if false == isMelodyErr || nil == hookErr || leaderGateHookPanickedReason != hookErr.Context()["reason"] {
        return "", false
    }

    hookName, _ := hookErr.Context()["hook"].(string)

    return hookName, true
}

const leaderGateHookPanickedReason = "leader_gate_hook_panicked"

/* runHookShielded runs a user hook behind a recover, since every hook runs on the bare Run goroutine, where a panic would end the process. The panic is journaled and answered as an error, so the term held for the hook can end. */
func (instance *LeaderGate) runHookShielded(runtimeInstance runtimecontract.Runtime, hookName string, hook func()) (hookErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        hookErr = exception.NewError(
            "leader gate hook panicked",
            exceptioncontract.Context{
                "reason":         leaderGateHookPanickedReason,
                "hook":           hookName,
                "name":           instance.name,
                "recoveredValue": recoveredValue,
                "panicStack":     string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )

        instance.gateLogger(runtimeInstance).Error(
            "leader gate hook panicked",
            exception.LogContext(hookErr, exception.LogContextConfig{}),
        )
    }()

    hook()

    return nil
}

func (instance *LeaderGate) gateLogger(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if true == internal.IsNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    return logger
}

/* lead holds the leadership term: it starts the lease renewal, runs OnElected underneath it, and blocks until the run context ends (nil), a renewal fails, or OnElected panics (the cause), so the gate demotes and campaigns again. The renewal runs before OnElected is invoked, so a hook that outlasts the ttl never leaves the lease unrenewed; a panicking hook ends the term, so no gate renews a lease under no work. */
func (instance *LeaderGate) lead(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    termContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    termRuntime := runtime.New(termContext, runtimeInstance.Scope(), runtimeInstance.Container())

    var refreshFailure error
    var waitGroup sync.WaitGroup

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        /* a panicking backend Refresh would kill the process with the lock held; recovered, it is the same demotion signal a returned error is */
        defer func() {
            if recoveredValue := recover(); nil != recoveredValue {
                panicValue, panicStack := recoveredRefreshPanic(recoveredValue)
                refreshFailure = exception.NewError(
                    "leader gate refresh panicked",
                    exceptioncontract.Context{"name": instance.name, "recoveredValue": panicValue, "panicStack": panicStack},
                    exception.PanicCause(panicValue),
                )
                instance.leaveTerm()
                cancel()
            }
        }()

        refreshFailure = instance.refreshWhileLeading(termRuntime, lock, func() {
            instance.leaveTerm()
            cancel()
        })
        if nil != refreshFailure {
            /* the lease is gone, so the claim is dropped here rather than where the term unwinds, which waits for OnElected to return; ending the term then stops OnElected */
            instance.leaveTerm()
            cancel()
        }
    }()

    var hookFailure error
    if nil != instance.settings.onElected {
        hookFailure = instance.runHookShielded(termRuntime, "OnElected", func() {
            instance.settings.onElected(termRuntime)
        })
        if nil != hookFailure {
            instance.leaveTerm()
            cancel()
        }
    }

    <-termContext.Done()

    /* cancel before Wait, so a renewal blocked on an unresponsive backend is interrupted rather than wedging the campaign loop */
    cancel()
    waitGroup.Wait()

    /* a lost lease is the stronger fact, and the failure the operator sees first */
    if nil != refreshFailure {
        return refreshFailure
    }

    return hookFailure
}

/* refreshWhileLeading renews the held lease at the configured cadence until the term context ends, returning the renewal failure that ends the term. The lease clock is the demotion's: once the lease last written reaches its demotion instant, half a cadence before it lapses, with no renewal landed, the term ends — by a timer, whatever the store is doing, with every renewal bounded to that instant — so OnElected is told to stop while nobody else can yet be elected. A failed renewal is retried once, halfway to that instant; each landed renewal moves the lease, dated from the instant it was issued. */
func (instance *LeaderGate) refreshWhileLeading(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock, demote func()) error {
    refreshTtl := instance.settings.ttl
    if true == instance.settings.sessionMode {
        /* session mode: a session locker ignores this ttl, while a lease locker rewrites the lease, so it renews for twice the probe cadence (see sessionProbeTtlFactor) */
        refreshTtl = sessionProbeTtlFactor * instance.settings.refreshInterval
    }

    refreshTimeout := resolveRefreshTimeout(instance.settings.refreshInterval)

    ticker := time.NewTicker(instance.settings.refreshInterval)
    defer ticker.Stop()

    /* enterTerm wrote the lease before this goroutine started, and leaveTerm zeroes it; a zero here is a term already left, whose stop is not a lost lease */
    leaseExpiryOffset := instance.leaseExpiry.Load()
    if false == instance.settings.sessionMode && 0 == leaseExpiryOffset {
        return nil
    }

    /* in session mode there is no lease, and only the consecutive-failure threshold ends the term */
    clock := newLeaseDemotionClock(
        false == instance.settings.sessionMode,
        false == instance.settings.sessionMode,
        instance.timeAnchor.Add(time.Duration(leaseExpiryOffset)),
        instance.settings.refreshInterval,
        refreshTimeout,
    )
    defer clock.stop()

    consecutiveFailureCount := 0

    for {
        select {
        case <-runtimeInstance.Context().Done():
            return nil
        case <-clock.demotion():
            /* select picks at random among ready cases: a term the run context already ended has no lease left to lose */
            if nil != runtimeInstance.Context().Err() {
                return nil
            }

            return clock.demotionError()
        case <-ticker.C:
        case <-clock.retry():
        }

        refreshIssuedAt := time.Now()

        renewalBudget, withinLease := clock.renewalBudget(refreshIssuedAt)
        if false == withinLease {
            return clock.demotionError()
        }

        demoted, refreshErr := clock.renew(
            func() error {
                return refreshOnce(runtimeInstance, lock, refreshTtl, renewalBudget)
            },
            demote,
            func() bool {
                return nil != runtimeInstance.Context().Err()
            },
        )
        if true == demoted {
            return clock.demotionError()
        }

        if nil == refreshErr {
            consecutiveFailureCount = 0

            if false == instance.settings.sessionMode {
                instance.leaseExpiry.Store(instance.leaseExpiryOffset(refreshIssuedAt, refreshTtl))
            }

            clock.landed(refreshIssuedAt.Add(refreshTtl))

            continue
        }

        /* a shutdown cancels the context the backend is called with, so a renewal failing with the cancellation is the stop itself, not a lost lease */
        if nil != runtimeInstance.Context().Err() {
            return nil
        }

        consecutiveFailureCount = consecutiveFailureCount + 1

        if true == clock.failed(time.Now(), refreshErr) || true == instance.refreshFailureEndsTheTerm(consecutiveFailureCount) {
            return refreshErr
        }
    }
}

/* refreshFailureEndsTheTerm answers whether a failed renewal ends the term. The lease clock is the authority: until the lease last written lapses, the store refuses the lock to everyone else, so a failed renewal has cost nothing yet. The consecutive-failure threshold covers a cadence far denser than the lease; in session mode there is no lease clock, and only the threshold decides. */
func (instance *LeaderGate) refreshFailureEndsTheTerm(consecutiveFailureCount int) bool {
    if false == instance.settings.sessionMode {
        leaseExpiry := instance.timeAnchor.Add(time.Duration(instance.leaseExpiry.Load()))
        if true == leaseIsBeyondRecovery(time.Now(), leaseExpiry, instance.settings.refreshInterval) {
            return true
        }
    }

    if true == instance.settings.refreshFailuresUnlimited {
        return false
    }

    return consecutiveFailureCount >= instance.settings.maxConsecutiveRefreshFailures
}

/* nextCampaignBackoff doubles the backoff after an acquire error and caps it at maxCampaignBackoff, never below the configured RetryInterval, so an outage is never retried faster than the healthy cadence. An overflowed doubling is floored back to the cap. */
func nextCampaignBackoff(current time.Duration, retryInterval time.Duration, maxCampaignBackoff time.Duration) time.Duration {
    backoffCap := maxCampaignBackoff
    if retryInterval > backoffCap {
        backoffCap = retryInterval
    }

    next := current * 2
    if next > backoffCap || 0 >= next {
        next = backoffCap
    }

    return next
}

func sleepUnlessDone(runContext interface{ Done() <-chan struct{} }, delay time.Duration) bool {
    timer := time.NewTimer(delay)
    defer timer.Stop()

    select {
    case <-runContext.Done():
        return false
    case <-timer.C:
        return true
    }
}
