package amqp

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* ReconnectConfig is the backoff a transport or a backplane re-dials and re-subscribes with after its connection drops. Each field left out is the parent's: the provider's for a component the provider built, the package default for one built directly, and the package default for the provider itself. */
type ReconnectConfig struct {
    /* InitialBackoff is the first wait: 1s by default, at least 1ms, unlimited refused, and never above MaxBackoff. */
    InitialBackoff bound.Duration

    /* MaxBackoff caps every wait: 30s by default, at least 1ms, unlimited refused, since a consumer that never re-subscribes stops for good. */
    MaxBackoff bound.Duration

    /* BackoffFactor grows the wait from one attempt to the next: 2 when left out, exactly 1 a constant backoff, below 1 or NaN refused, since a factor under 1 decays the wait into a reconnect storm. */
    BackoffFactor float64
}

var reconnectInitialBackoffRule = bound.Rule[time.Duration]{
    Name:    "ReconnectConfig.InitialBackoff",
    Default: time.Second,
    Minimum: bound.TimeoutMinimum,
}

var reconnectMaxBackoffRule = bound.Rule[time.Duration]{
    Name:    "ReconnectConfig.MaxBackoff",
    Default: 30 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

const defaultReconnectBackoffFactor = 2.0

type resolvedReconnect struct {
    initialBackoff time.Duration
    maxBackoff     time.Duration
    backoffFactor  float64
}

var defaultReconnect = resolvedReconnect{
    initialBackoff: reconnectInitialBackoffRule.Default,
    maxBackoff:     reconnectMaxBackoffRule.Default,
    backoffFactor:  defaultReconnectBackoffFactor,
}

/* resolveReconnectOver resolves override over an already resolved parent: a field left out takes the parent's value, a present one is resolved by its rule, and the cap and the factor are judged on the merged result, so a component that lowers only the cap under an inherited initial backoff is refused naming both. */
func resolveReconnectOver(parent resolvedReconnect, override ReconnectConfig) (resolvedReconnect, error) {
    resolved := parent

    if false == override.InitialBackoff.IsAbsent() {
        initialBackoff, initialBackoffErr := bound.Resolve(override.InitialBackoff, reconnectInitialBackoffRule)
        if nil != initialBackoffErr {
            return resolvedReconnect{}, initialBackoffErr
        }

        resolved.initialBackoff = initialBackoff.Value
    }

    if false == override.MaxBackoff.IsAbsent() {
        maxBackoff, maxBackoffErr := bound.Resolve(override.MaxBackoff, reconnectMaxBackoffRule)
        if nil != maxBackoffErr {
            return resolvedReconnect{}, maxBackoffErr
        }

        resolved.maxBackoff = maxBackoff.Value
    }

    if resolved.maxBackoff < resolved.initialBackoff {
        return resolvedReconnect{}, exception.NewError(
            "the initial backoff exceeds the maximum backoff",
            exceptioncontract.Context{
                "field":    "ReconnectConfig.InitialBackoff",
                "value":    resolved.initialBackoff.String(),
                "maximum":  resolved.maxBackoff.String(),
                "cappedBy": "ReconnectConfig.MaxBackoff",
            },
            nil,
        )
    }

    if 0 != override.BackoffFactor {
        /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN factor through */
        if false == (override.BackoffFactor >= 1) {
            return resolvedReconnect{}, exception.NewError(
                "the backoff factor is below 1",
                exceptioncontract.Context{
                    "field": "ReconnectConfig.BackoffFactor",
                    "value": strconv.FormatFloat(override.BackoffFactor, 'g', -1, 64),
                },
                nil,
            )
        }

        resolved.backoffFactor = override.BackoffFactor
    }

    return resolved, nil
}

func nextReconnectBackoff(reconnect resolvedReconnect, current time.Duration) time.Duration {
    /* clamp in float64 before the time.Duration conversion: a large BackoffFactor or MaxBackoff can push the product past the int64 nanosecond range, where the conversion wraps negative, slips past a later `next > maxBackoff` check and drives time.After into a no-delay reconnect storm */
    nextFloat := float64(current) * reconnect.backoffFactor
    if 0 >= nextFloat || nextFloat >= float64(reconnect.maxBackoff) {
        return reconnect.maxBackoff
    }

    return time.Duration(nextFloat)
}

/* a subscription resets the backoff only when it lived at least the initial backoff, so one that loses its channel immediately keeps backing off instead of storming the broker */
func reconnectBackoffShouldReset(reconnect resolvedReconnect, subscriptionDuration time.Duration) bool {
    return reconnect.initialBackoff <= subscriptionDuration
}
