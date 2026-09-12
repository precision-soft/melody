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

/* LeaderGateOptions tunes a LeaderGate; every zero value resolves to a sensible default derived from the ttl. */
/* defaultMaxConsecutiveRefreshFailures is the threshold a gate takes when its options name none. Three, because two consecutive losses are still comfortably a reconnect and the third says the store is not coming back inside a window that matters. At the default cadence it is unreachable — three renewals at half the ttl outlast the lease — so it changes nothing for a gate that did not ask for a denser cadence. */
const defaultMaxConsecutiveRefreshFailures = 3

type LeaderGateOptions struct {
    /* RetryInterval is the pause between failed campaigns while another instance leads; defaults to half the ttl, floored at one second. */
    RetryInterval time.Duration

    /* RefreshInterval is the lease-renewal cadence while leading; defaults to half the ttl, or defaultSessionProbeInterval when the ttl is non-positive (session-style locks whose Refresh is a liveness probe). */
    RefreshInterval time.Duration

    /* OnElected runs on the Run goroutine right after the gate becomes leader, with the lease renewal already running underneath it. Its runtime carries a context cancelled when the lease is lost, so leader-only work stops instead of running alongside whoever holds the lock now; while it blocks, the gate cannot campaign again. */
    OnElected func(runtimeInstance runtimecontract.Runtime)

    /* OnLost runs on the Run goroutine right after leadership is lost to a failed renewal; cause is the renewal error. It does not run on a clean shutdown. Left nil, the gate logs the lost term through the runtime's logger instead; wiring the hook replaces that record. */
    OnLost func(runtimeInstance runtimecontract.Runtime, cause error)

    /* MaxConsecutiveRefreshFailures is how many renewals in a row may fail before the gate leaves its term, whatever the lease still says. Zero takes the default of three; a negative value removes the threshold and leaves the lease clock as the only signal — and in session mode, which has no lease clock, a negative value means probe failures never end the term at all, which is the caller explicitly declining both signals.

       It exists because the two things a failed renewal can mean are not the same. A store that dropped a connection and reconnected is transient — and the lease it wrote is still the store's own promise that nobody else gets this lock until it lapses, which is why the cadence is half the lease: a lost renewal is meant to be survivable. Leaving on the first one turns an eight-second failover into a cancelled term, a re-election and work restarted from the beginning, for a lock that was never in danger.

       A store that is simply gone is not transient, and there the gate should stop being useful rather than wait out a lease it will certainly not renew. The threshold is what tells the two apart in the one configuration where the lease clock cannot: at the default cadence of half the ttl, three failures already outlast the lease, so the lease clock decides and this never fires. It bites only where the cadence is deliberately much denser than the lease, which is itself the operator saying they want to hear about failure quickly. */
    MaxConsecutiveRefreshFailures int

    /* OnCampaignError runs on the Run goroutine for every campaign that could not even ask the store who leads — the gate then backs off and campaigns again. Left nil, the gate logs each failed campaign through the runtime's logger, because a store outage and a permanent misconfiguration (a redis locker built with a non-positive ttl, whose Acquire fails closed on every call) are indistinguishable from the outside: both look exactly like a deployment that quietly elects no leader and does no work. Wiring the hook replaces that record. */
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
        /* a renewal cadence slower than half the lease lets the lease lapse before the first refresh, so a second instance can acquire and both report leadership: clamp an over-long override down to the safe derived cadence */
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

    /* timeAnchor is the construction instant, kept with its monotonic reading: the lease deadline is stored as an offset from it, so every lease comparison is a monotonic-clock difference. Stored as wall-clock unix nanoseconds instead, a backwards clock step (an ntp correction) would extend perceived leadership past the real lease — IsLeader would keep answering true while a second instance legally acquires, and the failure-recovery margin would refuse to demote for the whole step. */
    timeAnchor time.Time

    /* inTerm marks a term the gate has entered and not yet left; leaseExpiry carries the instant the held lease lapses, as nanoseconds since timeAnchor, and is zero outside a term. Both are needed: the term flag alone cannot expire on its own, and the deadline alone cannot tell an untaken lock from one whose lease is still running out after the term ended. */
    inTerm      atomic.Bool
    leaseExpiry atomic.Int64
}

func (instance *LeaderGate) leaseExpiryOffset(issuedAt time.Time, ttl time.Duration) int64 {
    return int64(issuedAt.Sub(instance.timeAnchor) + ttl)
}

/* IsLeader answers from the lease rather than from the last renewal's verdict: the gate leads while it is inside a term AND the lease it took or last renewed is still in the future. A renewal that never answers — a store that accepted the call and went quiet — returns no error to demote on, so a term flag on its own keeps reporting leadership long after the lease lapsed and a second instance acquired it; a deadline expires by itself, with nothing to wait for. A non-positive ttl is session mode (MySQL GET_LOCK, PostgreSQL advisory): the lock lives as long as the connection does, there is no lease to outlive, and the term is the whole answer. */
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

/* enterTerm publishes the lease before the term, so no reader can see leadership backed by the deadline of a term that already ended. The lease is dated from the instant the acquire was ISSUED, not from when it answered: the store started the lease somewhere inside that call, and dating it from the later instant would claim time the lease does not have. In session mode there is no lease at all — the offset stays zero, IsLeader answers from the term alone and refreshFailureEndsTheTerm never consults the lease clock. */
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

    runContext := runtimeInstance.Context()
    campaignBackoff := instance.options.RetryInterval

    for {
        if nil != runContext.Err() {
            return nil
        }

        /* a fresh lock per campaign: every CreateLock mints a new fencing token, and reusing a lock after losing it would alias tokens with the holder that took it over */
        lock := instance.locker.CreateLock(instance.name, instance.ttl)

        acquireIssuedAt := time.Now()
        acquired, acquireErr := lock.Acquire(runtimeInstance)
        if nil != acquireErr {
            /* a shutdown cancels the very context the backend was called with, so the campaign in flight fails with the cancellation: that is the stop itself, and reporting it would hand every graceful shutdown an error that reads like a store outage */
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

/* holdTerm enters the term, leads it, and leaves it with the lock released — through defers, so a panicking OnElected hook (or a panicking backend) unwinding through here still drops the claim and still releases the lock, instead of killing the process with the lock held for the rest of its ttl on every peer. */
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

/* reportCampaignError hands a failed campaign to the OnCampaignError hook when one is wired, and records it itself otherwise: without a record a store outage and a permanent misconfiguration are both indistinguishable from a deployment that quietly elects no leader and does no work. A wired hook replaces the record rather than adding to it, so an observer that routes the failure elsewhere is not echoed. */
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

/* reportLost mirrors reportCampaignError for a term lost to a failed renewal. */
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

/* runHookShielded runs a user hook behind a recover, because every hook runs on the Run goroutine and Run's own documentation says to start it bare: an unshielded panic would unwind a goroutine with no recover and take the whole process down on user code the gate merely notifies. */
func (instance *LeaderGate) runHookShielded(runtimeInstance runtimecontract.Runtime, hookName string, hook func()) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        recoveredErr, _ := recoveredValue.(error)

        instance.gateLogger(runtimeInstance).Error(
            "leader gate hook panicked",
            exception.LogContext(
                exception.NewError(
                    "leader gate hook panicked",
                    exceptioncontract.Context{
                        "hook":           hookName,
                        "name":           instance.name,
                        "recoveredValue": recoveredValue,
                        "panicStack":     string(debug.Stack()),
                    },
                    recoveredErr,
                ),
            ),
        )
    }()

    hook()
}

func (instance *LeaderGate) gateLogger(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        logger = logging.EmergencyLogger()
    }

    return logger
}

/* lead holds the leadership term: it starts the lease renewal, runs OnElected underneath it, and blocks until the run context ends (returns nil) or a renewal fails (returns the cause, so the gate demotes itself and re-campaigns). The renewal must be running before OnElected is invoked — nothing would renew the lease while the hook works, so a hook that merely takes longer than the ttl lets a second instance acquire while this one still reports leadership and never demotes, since demotion only follows a failed renewal. */
func (instance *LeaderGate) lead(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    termContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    termRuntime := runtime.New(termContext, runtimeInstance.Scope(), runtimeInstance.Container())

    var refreshFailure error
    var waitGroup sync.WaitGroup

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        /* a panicking backend Refresh would otherwise unwind a bare goroutine and kill the process with the lock still held; recovered, it is the same demotion signal a returned error is */
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
            /* the lease is gone. The claim is dropped here rather than where the term unwinds, because the term unwinds only once OnElected returns — a hook that takes its time, or one that ignores the cancelled context, would otherwise keep IsLeader answering true for a lock this instance provably no longer holds, and LOCK.md invites callers to combine the two signals for one fact. Ending the term then stops OnElected rather than let leader-only work continue alongside whoever holds the lock now. */
            instance.leaveTerm()
            cancel()
        }
    }()

    if nil != instance.options.OnElected {
        instance.runHookShielded(termRuntime, "OnElected", func() {
            instance.options.OnElected(termRuntime)
        })
    }

    <-termContext.Done()

    /* cancel before Wait so a renewal already blocked on an unresponsive backend is interrupted rather than wedging the campaign loop until the connection times out */
    cancel()
    waitGroup.Wait()

    return refreshFailure
}

/* refreshWhileLeading renews the held lease at the configured cadence until the term context ends, returning the first renewal failure. Every renewal is issued under a deadline of its own (resolveRefreshTimeout), because a call that never answers is the one failure mode this loop cannot otherwise see: it would sit in Refresh while the lease lapses and a second instance takes the lock, with no error to return and nothing to demote on. Each renewal that lands moves the lease deadline IsLeader answers from, dated from the instant the call was issued rather than from when it answered, so the claim never outlives the lease the store actually wrote. */
func (instance *LeaderGate) refreshWhileLeading(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    refreshTtl := instance.ttl
    if 0 >= refreshTtl {
        /* session mode: a session locker ignores this ttl, while a lease locker rewrites the lease — renew for twice the probe cadence so the renewal never races the expiry it just set (see sessionProbeTtlFactor) */
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
                /* a shutdown cancels the very context the backend was called with, so the renewal in flight fails with the cancellation: that is the stop itself, not a lost lease, and reporting it would drive OnLost on every clean stop */
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

/* refreshFailureEndsTheTerm answers the one question a failed renewal poses: is the lock still ours to hold? Two things say no, and they answer different failures.

   The lease clock is the authority. Until the lease this gate last wrote lapses, the store refuses the lock to everyone else whether or not this process can still reach it — so a renewal that failed while the lease runs has cost nothing yet, and leaving on it gives up availability for no exclusivity gained.

   The consecutive-failure threshold covers what the lease clock cannot see: a gate whose cadence is far denser than its lease would otherwise keep working for the whole lease against a store that has plainly gone. At the default cadence the threshold is unreachable and the lease decides.

   In session mode (a non-positive ttl) there is no lease and no lease clock: the lock lives as long as the backend session does, so only the threshold decides. Consulting the lease clock there would read a deadline that was never written — the zero offset dates to the gate's construction, so the FIRST failed probe of every term would demote immediately, overriding the documented three-failure tolerance and even a negative never-on-count threshold. */
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

/* nextCampaignBackoff doubles the campaign backoff after an acquire error and caps it, but never below the configured RetryInterval: a RetryInterval slower than defaultMaxCampaignBackoff must never make outage retries faster than the healthy campaign cadence (which would hammer an already-struggling store and spam OnCampaignError). It also floors an overflowed doubling back to the cap. */
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
