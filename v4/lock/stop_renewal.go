package lock

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

/* defaultStopRenewalLimit is how long a lease is still renewed after the stop when the configuration names no limit. The runtime does not answer the application's teardown timeout (10s by default), so the literal stands; it equals Kubernetes' default grace period, so a deployment on defaults sets it below its own forced exit. */
const defaultStopRenewalLimit = 30 * time.Second

func stopRenewalLimitRule(name string) bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              name,
        Default:           defaultStopRenewalLimit,
        Minimum:           bound.TimeoutMinimum,
        UnlimitedAdmitted: true,
    }
}

/* resolveStopRenewalLimit resolves the limit and refuses one set below the refresh interval, which could not land one renewal after the stop; a limit left out is raised to the interval instead, so a long lease keeps its working default */
func resolveStopRenewalLimit(field string, configured bound.Duration, refreshInterval time.Duration) (bound.Resolved[time.Duration], error) {
    limit, limitErr := bound.Resolve(configured, stopRenewalLimitRule(field))
    if nil != limitErr {
        return limit, limitErr
    }

    if true == limit.Unlimited || limit.Value >= refreshInterval {
        return limit, nil
    }

    if true == configured.IsAbsent() {
        limit.Value = refreshInterval

        return limit, nil
    }

    return limit, exception.NewError(
        "the stop renewal limit is below the refresh interval",
        exceptioncontract.Context{
            "field":           field,
            "value":           limit.Value.String(),
            "refreshInterval": refreshInterval.String(),
        },
        nil,
    )
}

/* stopRenewalWatch keeps the lease renewed while the work holding it winds down after a stop: it arms, when the stop arrives, the limit past which the renewal ends and the lease lapses by itself. */
type stopRenewalWatch struct {
    stopContext context.Context
    limit       bound.Resolved[time.Duration]
    timer       *time.Timer
    armed       bool
}

/* a session-style lock has no lease to lapse, so its probe runs until the work returns and the limit is not armed */
func newStopRenewalWatch(stopContext context.Context, limit bound.Resolved[time.Duration], sessionMode bool) *stopRenewalWatch {
    if true == sessionMode {
        limit = bound.Resolved[time.Duration]{Unlimited: true}
    }

    return &stopRenewalWatch{stopContext: stopContext, limit: limit}
}

/* stopped is the stop signal until the watch is armed, nil afterwards */
func (instance *stopRenewalWatch) stopped() <-chan struct{} {
    if true == instance.armed {
        return nil
    }

    return instance.stopContext.Done()
}

func (instance *stopRenewalWatch) arm() {
    instance.armed = true

    if false == instance.limit.Unlimited {
        instance.timer = time.NewTimer(instance.limit.Value)
    }
}

/* expired fires once the limit has passed since the stop; nil while unarmed or unlimited */
func (instance *stopRenewalWatch) expired() <-chan time.Time {
    if nil == instance.timer {
        return nil
    }

    return instance.timer.C
}

/* passed takes the stop and the limit ahead of a renewal tick ready in the same select, so no renewal is issued once the limit has passed */
func (instance *stopRenewalWatch) passed() bool {
    select {
    case <-instance.stopped():
        instance.arm()
    default:
    }

    select {
    case <-instance.expired():
        return true
    default:
        return false
    }
}

func (instance *stopRenewalWatch) stop() {
    if nil != instance.timer {
        instance.timer.Stop()
    }
}

/* expiredError writes the warning at the instant the limit passes, since the work may never return to carry the error, and answers it */
func (instance *stopRenewalWatch) expiredError(runtimeInstance containercontract.Runtime, name string, field string) error {
    expiredErr := exception.NewError(
        "the work was still running past the stop renewal limit; the lease is no longer renewed",
        exceptioncontract.Context{
            "name":  name,
            "field": field,
            "limit": instance.limit.Value.String(),
        },
        nil,
    )

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if true == internal.IsNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    logger.Warning("lock lease no longer renewed: the work is still running past the stop renewal limit, a peer may acquire the lock", exception.LogContext(expiredErr, exception.LogContextConfig{}))

    return expiredErr
}
