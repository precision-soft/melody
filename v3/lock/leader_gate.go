package lock

import (
    "context"
    "runtime/debug"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const defaultLeaderRetryFloor = 1 * time.Second

/* defaultMaxCampaignBackoff caps the doubling delay between failed campaigns after acquire ERRORS (a store outage), so a persistent outage neither tight-loops the gate nor pushes re-election out indefinitely. */
const defaultMaxCampaignBackoff = 1 * time.Minute

/* defaultMaxConsecutiveRefreshFailures is the threshold a gate takes when its options name none. At the default cadence it is unreachable, since three renewals at half the ttl outlast the lease. */
const defaultMaxConsecutiveRefreshFailures = 3

/* LeaderGateOptions tunes a LeaderGate; every zero value resolves to a default derived from the ttl. */
type LeaderGateOptions struct {
    /* RetryInterval is the pause between failed campaigns while another instance leads; defaults to half the ttl, floored at one second. */
    RetryInterval time.Duration

    /* RefreshInterval is the lease-renewal cadence while leading; defaults to half the ttl, or defaultSessionProbeInterval when the ttl is non-positive (session-style locks whose Refresh is a liveness probe). */
    RefreshInterval time.Duration

    /* OnElected runs on the Run goroutine right after the gate becomes leader, with the lease renewal already running. Its runtime carries a context cancelled when the lease is lost, and while it blocks the gate cannot campaign again. A panic out of it ends the term: the lock is released, OnLost receives the panic as the cause, and the gate campaigns again after RetryInterval. */
    OnElected func(runtimeInstance runtimecontract.Runtime)

    /* OnLost runs on the Run goroutine right after leadership is lost — to a failed renewal, whose error is the cause, or to a panic out of OnElected, whose recovered value reaches it as the cause of a "leader gate hook panicked" error; when both happen in one term the renewal error wins and the panic is journaled beside it. It does not run on a clean shutdown. Left nil, the gate logs the lost term through the runtime's logger instead; wiring the hook replaces that record. */
    OnLost func(runtimeInstance runtimecontract.Runtime, cause error)

    /* MaxConsecutiveRefreshFailures is how many renewals in a row may fail before the gate leaves its term, whatever the lease still says. Zero takes the default of three; a negative value leaves the lease clock as the only signal, and in session mode, which has no lease clock, means probe failures never end the term. The threshold bites only where the cadence is much denser than the lease: at the default cadence of half the ttl the lease clock decides first. */
    MaxConsecutiveRefreshFailures int

    /* OnCampaignError runs on the Run goroutine for every campaign that could not ask the store who leads; the gate then backs off and campaigns again. Left nil, the gate logs each failed campaign, since an outage and a misconfiguration both look like a deployment that elects no leader. Wiring the hook replaces that record. */
    OnCampaignError func(runtimeInstance runtimecontract.Runtime, cause error)
}

func NewLeaderGate(locker lockcontract.Locker, name string, ttl time.Duration) *LeaderGate {
    return NewLeaderGateWithOptions(locker, name, ttl, LeaderGateOptions{})
}

func NewLeaderGateWithOptions(
    locker lockcontract.Locker,
    name string,
    ttl time.Duration,
    options LeaderGateOptions,
) *LeaderGate {
    if true == internal.IsNilInterface(locker) {
        exception.Panic(exception.NewError("leader gate locker is nil", nil, nil))
    }

    if "" == name {
        exception.Panic(exception.NewError("leader gate lock name is empty", nil, nil))
    }

    resolved := options
    if 0 >= resolved.RetryInterval {
        resolved.RetryInterval = ttl / 2
        if defaultLeaderRetryFloor > resolved.RetryInterval {
            resolved.RetryInterval = defaultLeaderRetryFloor
        }
    }
    if 0 >= resolved.RefreshInterval {
        resolved.RefreshInterval = defaultSessionProbeInterval
        if 0 < ttl {
            resolved.RefreshInterval = ttl / 2
        }
    }
    if 0 < ttl && resolved.RefreshInterval > ttl/2 {
        /* a cadence slower than half the lease lets the lease lapse before the first refresh, so a second instance could lead too: an over-long override is clamped to the derived cadence */
        resolved.RefreshInterval = ttl / 2
    }
    if minimumRefreshInterval > resolved.RefreshInterval {
        resolved.RefreshInterval = minimumRefreshInterval
    }
    if 0 == resolved.MaxConsecutiveRefreshFailures {
        resolved.MaxConsecutiveRefreshFailures = defaultMaxConsecutiveRefreshFailures
    }

    return &LeaderGate{
        locker:     locker,
        name:       name,
        ttl:        ttl,
        options:    resolved,
        timeAnchor: time.Now(),
    }
}

/* LeaderGate is the become-leader, renew-periodically, release-on-shutdown pattern over any lock backend: Run campaigns for the named lock, holds and renews it while leading, demotes itself and re-campaigns when a renewal fails, and releases the lock on shutdown. Wrap the work itself in a check on IsLeader, or hook OnElected/OnLost. A non-positive ttl selects session-style locks (MySQL GET_LOCK, PostgreSQL advisory): there is no lease to extend, so the renewal is a liveness probe. */
type LeaderGate struct {
    locker  lockcontract.Locker
    name    string
    ttl     time.Duration
    options LeaderGateOptions

    /* the lease deadline is stored as an offset from this monotonic instant, so a backwards wall-clock step cannot extend perceived leadership past the real lease */
    timeAnchor time.Time

    /* inTerm marks a term entered and not yet left; leaseExpiry is the instant the held lease lapses, as nanoseconds since timeAnchor, and zero outside a term. The flag alone cannot expire, and the deadline alone cannot tell an untaken lock from a lease still running out. */
    inTerm      atomic.Bool
    leaseExpiry atomic.Int64
}

func (instance *LeaderGate) leaseExpiryOffset(issuedAt time.Time, ttl time.Duration) int64 {
    return int64(issuedAt.Sub(instance.timeAnchor) + ttl)
}

/* IsLeader answers from the lease: the gate leads while it is inside a term and the lease it took or last renewed is still in the future, so a renewal that never answers cannot keep it leading. In session mode (a non-positive ttl) there is no lease, and the term is the whole answer. */
func (instance *LeaderGate) IsLeader() bool {
    if false == instance.inTerm.Load() {
        return false
    }

    if 0 >= instance.ttl {
        return true
    }

    expiry := instance.leaseExpiry.Load()

    return 0 != expiry && int64(time.Since(instance.timeAnchor)) < expiry
}

/* enterTerm publishes the lease before the term, so no reader sees leadership backed by an ended term's deadline. The lease is dated from the instant the acquire was issued, since the store started it somewhere inside the call; in session mode the offset stays zero. */
func (instance *LeaderGate) enterTerm(acquireIssuedAt time.Time) {
    if 0 < instance.ttl {
        instance.leaseExpiry.Store(instance.leaseExpiryOffset(acquireIssuedAt, instance.ttl))
    }
    instance.inTerm.Store(true)
}

func (instance *LeaderGate) leaveTerm() {
    instance.inTerm.Store(false)
    instance.leaseExpiry.Store(0)
}

/* Run blocks until the runtime context is cancelled and always returns nil on a clean shutdown; start it with `go gate.Run(runtimeInstance)` for a long-running worker. Acquire errors (a store outage) never abort it — they back off doubling, capped at defaultMaxCampaignBackoff, and campaigning resumes. Because they never abort it, they are also never returned: each one is logged through the runtime's logger unless OnCampaignError is wired, in which case the hook owns the record. */
func (instance *LeaderGate) Run(runtimeInstance runtimecontract.Runtime) error {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(exception.NewError("leader gate runtime is nil", nil, nil))
    }

    refuseARuntimeThatCannotBeRewrapped(runtimeInstance, "leader gate")

    runContext := runtimeInstance.Context()
    campaignBackoff := instance.options.RetryInterval

    for {
        if nil != runContext.Err() {
            return nil
        }

        /* a fresh lock per campaign: every CreateLock mints a new fencing token, and a lock reused after losing it would alias the tokens of the holder that took it over */
        lock := instance.locker.CreateLock(instance.name, instance.ttl)

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

            campaignBackoff = nextCampaignBackoff(campaignBackoff, instance.options.RetryInterval)

            continue
        }

        campaignBackoff = instance.options.RetryInterval

        if false == acquired {
            if false == sleepUnlessDone(runContext, instance.options.RetryInterval) {
                return nil
            }

            continue
        }

        lostCause := instance.holdTerm(runtimeInstance, lock, acquireIssuedAt)

        if nil != runContext.Err() {
            return nil
        }

        instance.reportLost(runtimeInstance, lostCause)

        if false == sleepUnlessDone(runContext, instance.options.RetryInterval) {
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

    defer releaseDetached(runtimeInstance, lock, instance.name)
    defer instance.leaveTerm()

    return instance.lead(runtimeInstance, lock)
}

/* reportCampaignError hands a failed campaign to the OnCampaignError hook when one is wired, and records it itself otherwise; a wired hook replaces the record. */
func (instance *LeaderGate) reportCampaignError(runtimeInstance runtimecontract.Runtime, cause error) {
    if nil != instance.options.OnCampaignError {
        instance.runHookShielded(runtimeInstance, "OnCampaignError", func() {
            instance.options.OnCampaignError(runtimeInstance, cause)
        })

        return
    }

    instance.gateLogger(runtimeInstance).Warning(
        "leader gate campaign failed; backing off and campaigning again",
        exception.LogContext(cause, exceptioncontract.Context{"name": instance.name}),
    )
}

func (instance *LeaderGate) reportLost(runtimeInstance runtimecontract.Runtime, cause error) {
    if nil != instance.options.OnLost {
        instance.runHookShielded(runtimeInstance, "OnLost", func() {
            instance.options.OnLost(runtimeInstance, cause)
        })

        return
    }

    instance.gateLogger(runtimeInstance).Warning(
        "leader gate lost its term; campaigning again",
        exception.LogContext(cause, exceptioncontract.Context{"name": instance.name}),
    )
}

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
                "hook":           hookName,
                "name":           instance.name,
                "recoveredValue": recoveredValue,
                "panicStack":     string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )

        instance.gateLogger(runtimeInstance).Error(
            "leader gate hook panicked",
            exception.LogContext(hookErr),
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
                refreshFailure = exception.NewError(
                    "leader gate refresh panicked",
                    exceptioncontract.Context{"name": instance.name, "recoveredValue": recoveredValue},
                    nil,
                )
                instance.leaveTerm()
                cancel()
            }
        }()

        refreshFailure = instance.refreshWhileLeading(termRuntime, lock)
        if nil != refreshFailure {
            /* the lease is gone, so the claim is dropped here rather than where the term unwinds, which waits for OnElected to return; ending the term then stops OnElected */
            instance.leaveTerm()
            cancel()
        }
    }()

    var hookFailure error
    if nil != instance.options.OnElected {
        hookFailure = instance.runHookShielded(termRuntime, "OnElected", func() {
            instance.options.OnElected(termRuntime)
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

/* refreshWhileLeading renews the held lease at the configured cadence until the term context ends, returning the first renewal failure. Every renewal runs under its own deadline (resolveRefreshTimeout), so a call that never answers cannot sit while the lease lapses; each landed renewal moves the lease deadline, dated from the instant it was issued. */
func (instance *LeaderGate) refreshWhileLeading(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    refreshTtl := instance.ttl
    if 0 >= refreshTtl {
        /* session mode: a session locker ignores this ttl, while a lease locker rewrites the lease, so it renews for twice the probe cadence (see sessionProbeTtlFactor) */
        refreshTtl = sessionProbeTtlFactor * instance.options.RefreshInterval
    }

    refreshTimeout := resolveRefreshTimeout(instance.options.RefreshInterval)

    ticker := time.NewTicker(instance.options.RefreshInterval)
    defer ticker.Stop()

    consecutiveFailureCount := 0

    for {
        select {
        case <-runtimeInstance.Context().Done():
            return nil
        case <-ticker.C:
            refreshIssuedAt := time.Now()

            if refreshErr := refreshOnce(runtimeInstance, lock, refreshTtl, refreshTimeout); nil != refreshErr {
                /* a shutdown cancels the context the backend is called with, so a renewal failing with the cancellation is the stop itself, not a lost lease */
                if nil != runtimeInstance.Context().Err() {
                    return nil
                }

                consecutiveFailureCount = consecutiveFailureCount + 1

                if true == instance.refreshFailureEndsTheTerm(consecutiveFailureCount) {
                    return refreshErr
                }

                continue
            }

            consecutiveFailureCount = 0

            if 0 < instance.ttl {
                instance.leaseExpiry.Store(instance.leaseExpiryOffset(refreshIssuedAt, refreshTtl))
            }
        }
    }
}

/* refreshFailureEndsTheTerm answers whether a failed renewal ends the term. The lease clock is the authority: until the lease last written lapses, the store refuses the lock to everyone else, so a failed renewal has cost nothing yet. The consecutive-failure threshold covers a cadence far denser than the lease; in session mode there is no lease clock, and only the threshold decides. */
func (instance *LeaderGate) refreshFailureEndsTheTerm(consecutiveFailureCount int) bool {
    if 0 < instance.ttl {
        leaseExpiry := instance.timeAnchor.Add(time.Duration(instance.leaseExpiry.Load()))
        if true == leaseIsBeyondRecovery(time.Now(), leaseExpiry, instance.options.RefreshInterval) {
            return true
        }
    }

    if 0 >= instance.options.MaxConsecutiveRefreshFailures {
        return false
    }

    return consecutiveFailureCount >= instance.options.MaxConsecutiveRefreshFailures
}

/* nextCampaignBackoff doubles the backoff after an acquire error and caps it, never below the configured RetryInterval, so an outage is never retried faster than the healthy cadence. An overflowed doubling is floored back to the cap. */
func nextCampaignBackoff(current time.Duration, retryInterval time.Duration) time.Duration {
    backoffCap := defaultMaxCampaignBackoff
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
