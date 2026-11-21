package rueidis

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* ReconnectConfig is the backoff the backplane re-subscribes with after its subscription drops; its zero value is the default backoff. */
type ReconnectConfig struct {
    /* InitialBackoff is the first wait: 1s by default, at least 1ms, unlimited refused, and never above MaxBackoff. */
    InitialBackoff bound.Duration

    /* MaxBackoff caps every wait: 30s by default, at least 1ms, unlimited refused, since a backplane that never re-subscribes stops replicating for good. */
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

func resolveReconnect(config ReconnectConfig) (resolvedReconnect, error) {
    initialBackoff, initialBackoffErr := bound.Resolve(config.InitialBackoff, reconnectInitialBackoffRule)
    if nil != initialBackoffErr {
        return resolvedReconnect{}, initialBackoffErr
    }

    maxBackoff, maxBackoffErr := bound.Resolve(config.MaxBackoff, reconnectMaxBackoffRule)
    if nil != maxBackoffErr {
        return resolvedReconnect{}, maxBackoffErr
    }

    if maxBackoff.Value < initialBackoff.Value {
        return resolvedReconnect{}, exception.NewError(
            "the initial backoff exceeds the maximum backoff",
            exceptioncontract.Context{
                "field":    "ReconnectConfig.InitialBackoff",
                "value":    initialBackoff.Value.String(),
                "maximum":  maxBackoff.Value.String(),
                "cappedBy": "ReconnectConfig.MaxBackoff",
            },
            nil,
        )
    }

    backoffFactor := config.BackoffFactor
    if 0 == backoffFactor {
        backoffFactor = defaultReconnectBackoffFactor
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN factor through */
    if false == (backoffFactor >= 1) {
        return resolvedReconnect{}, exception.NewError(
            "the backoff factor is below 1",
            exceptioncontract.Context{
                "field": "ReconnectConfig.BackoffFactor",
                "value": strconv.FormatFloat(config.BackoffFactor, 'g', -1, 64),
            },
            nil,
        )
    }

    return resolvedReconnect{
        initialBackoff: initialBackoff.Value,
        maxBackoff:     maxBackoff.Value,
        backoffFactor:  backoffFactor,
    }, nil
}
