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

const defaultMaxCampaignBackoff = 1 * time.Minute

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

    /* MaxConsecutiveRefreshFailures limits failed renewals before leadership ends. Zero defaults to three. Negative values disable this threshold, leaving lease expiry as the remaining signal; session mode has no lease clock, so negative values disable both failure limits. */
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

    timeAnchor time.Time

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

        lock := instance.locker.CreateLock(instance.name, instance.ttl)

        acquireIssuedAt := time.Now()
        acquired, acquireErr := lock.Acquire(runtimeInstance)
        if nil != acquireErr {

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

func (instance *LeaderGate) lead(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    termContext, cancel := context.WithCancel(runtimeInstance.Context())
    defer cancel()

    termRuntime := runtime.New(termContext, runtimeInstance.Scope(), runtimeInstance.Container())

    var refreshFailure error
    var waitGroup sync.WaitGroup

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

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

    cancel()
    waitGroup.Wait()

    return refreshFailure
}

func (instance *LeaderGate) refreshWhileLeading(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    refreshTtl := instance.ttl
    if 0 >= refreshTtl {

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
