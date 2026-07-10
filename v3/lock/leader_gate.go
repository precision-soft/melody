package lock

import (
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const defaultLeaderRetryFloor = 1 * time.Second

/* defaultMaxCampaignBackoff caps the doubling delay between failed campaigns after acquire ERRORS (a store outage), so a persistent outage neither tight-loops the gate nor pushes re-election out indefinitely. */
const defaultMaxCampaignBackoff = 1 * time.Minute

/* LeaderGateOptions tunes a LeaderGate; every zero value resolves to a sensible default derived from the ttl. */
type LeaderGateOptions struct {
    /* RetryInterval is the pause between failed campaigns while another instance leads; defaults to half the ttl, floored at one second. */
    RetryInterval time.Duration

    /* RefreshInterval is the lease-renewal cadence while leading; defaults to half the ttl, or defaultSessionProbeInterval when the ttl is non-positive (session-style locks whose Refresh is a liveness probe). */
    RefreshInterval time.Duration

    /* OnElected runs on the Run goroutine right after the gate becomes leader. */
    OnElected func(runtimeInstance runtimecontract.Runtime)

    /* OnLost runs on the Run goroutine right after leadership is lost to a failed renewal; cause is the renewal error. It does not run on a clean shutdown. */
    OnLost func(runtimeInstance runtimecontract.Runtime, cause error)

    /* OnCampaignError runs on the Run goroutine for every campaign that could not even ask the store who leads — the gate then backs off and campaigns again, so without this hook the error is never seen. A store outage and a permanent misconfiguration (a redis locker built with a non-positive ttl, whose Acquire fails closed on every call) are indistinguishable from the outside: both look exactly like a deployment that quietly elects no leader and does no work. */
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
    if minimumRefreshInterval > resolved.RefreshInterval {
        resolved.RefreshInterval = minimumRefreshInterval
    }

    return &LeaderGate{
        locker:  locker,
        name:    name,
        ttl:     ttl,
        options: resolved,
    }
}

/* LeaderGate is the become-leader, renew-periodically, release-on-shutdown pattern over any lock backend: Run campaigns for the named lock, holds and renews it while leading, demotes itself and re-campaigns when a renewal fails, and releases the lock on shutdown. Wrap the work itself in a check on IsLeader, or hook OnElected/OnLost. A non-positive ttl selects session-style locks (MySQL GET_LOCK, PostgreSQL advisory): there is no lease to extend, so the renewal is a liveness probe. */
type LeaderGate struct {
    locker   lockcontract.Locker
    name     string
    ttl      time.Duration
    options  LeaderGateOptions
    isLeader atomic.Bool
}

func (instance *LeaderGate) IsLeader() bool {
    return instance.isLeader.Load()
}

/* Run blocks until the runtime context is cancelled and always returns nil on a clean shutdown; start it with `go gate.Run(runtimeInstance)` for a long-running worker. Acquire errors (a store outage) never abort it — they back off doubling, capped at defaultMaxCampaignBackoff, and campaigning resumes. Because they never abort it, they are also never returned: hook OnCampaignError to see them, or a permanent misconfiguration is indistinguishable from a deployment that simply has no work to lead. */
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

        acquired, acquireErr := lock.Acquire(runtimeInstance)
        if nil != acquireErr {
            /* a shutdown cancels the very context the backend was called with, so the campaign in flight fails with the cancellation: that is the stop itself, and reporting it would hand every graceful shutdown an error that reads like a store outage */
            if nil != runContext.Err() {
                return nil
            }

            if nil != instance.options.OnCampaignError {
                instance.options.OnCampaignError(runtimeInstance, acquireErr)
            }

            if false == sleepUnlessDone(runContext, campaignBackoff) {
                return nil
            }

            campaignBackoff = campaignBackoff * 2
            if campaignBackoff > defaultMaxCampaignBackoff || 0 >= campaignBackoff {
                campaignBackoff = defaultMaxCampaignBackoff
            }

            continue
        }

        campaignBackoff = instance.options.RetryInterval

        if false == acquired {
            if false == sleepUnlessDone(runContext, instance.options.RetryInterval) {
                return nil
            }

            continue
        }

        instance.isLeader.Store(true)
        if nil != instance.options.OnElected {
            instance.options.OnElected(runtimeInstance)
        }

        lostCause := instance.lead(runtimeInstance, lock)

        instance.isLeader.Store(false)
        releaseDetached(runtimeInstance, lock)

        if nil != runContext.Err() {
            return nil
        }

        if nil != instance.options.OnLost {
            instance.options.OnLost(runtimeInstance, lostCause)
        }

        if false == sleepUnlessDone(runContext, instance.options.RetryInterval) {
            return nil
        }
    }
}

/* lead renews the held lease until the context ends (returns nil) or a renewal fails (returns the cause, so the gate demotes itself and re-campaigns). */
func (instance *LeaderGate) lead(runtimeInstance runtimecontract.Runtime, lock lockcontract.Lock) error {
    refreshTtl := instance.ttl
    if 0 >= refreshTtl {
        /* session mode: a session locker ignores this ttl, while a lease locker rewrites the lease — renew for twice the probe cadence so the renewal never races the expiry it just set (see sessionProbeTtlFactor) */
        refreshTtl = sessionProbeTtlFactor * instance.options.RefreshInterval
    }

    ticker := time.NewTicker(instance.options.RefreshInterval)
    defer ticker.Stop()

    for {
        select {
        case <-runtimeInstance.Context().Done():
            return nil
        case <-ticker.C:
            if refreshErr := lock.Refresh(runtimeInstance, refreshTtl); nil != refreshErr {
                return refreshErr
            }
        }
    }
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
