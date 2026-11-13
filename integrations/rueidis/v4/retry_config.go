package rueidis

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* RetryConfig is the retry an open makes on a transient failure; its zero value is the default retry, and a single attempt is written as MaxAttempts bound.Of(1). */
type RetryConfig struct {
    /* MaxAttempts counts the attempts, the first one included: 3 by default, at least 1, unlimited refused, since an open retried for ever hides a dead store from the boot. */
    MaxAttempts bound.Count

    /* InitialDelay is the wait before the second attempt: 500ms by default, at least 1ms, unlimited refused. */
    InitialDelay bound.Duration

    /* MaxDelay caps the wait between two attempts: 5s by default, at least 1ms, unlimited refused. */
    MaxDelay bound.Duration

    /* BackoffMultiplier grows the wait from one attempt to the next: 2 when left out, exactly 1 a constant backoff, below 1 or NaN refused, since a multiplier under 1 decays the wait into a re-dial storm. */
    BackoffMultiplier float64
}

var retryMaxAttemptsRule = bound.Rule[int]{
    Name:    "RetryConfig.MaxAttempts",
    Default: 3,
    Minimum: 1,
}

var retryInitialDelayRule = bound.Rule[time.Duration]{
    Name:    "RetryConfig.InitialDelay",
    Default: 500 * time.Millisecond,
    Minimum: bound.TimeoutMinimum,
}

var retryMaxDelayRule = bound.Rule[time.Duration]{
    Name:    "RetryConfig.MaxDelay",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

const defaultBackoffMultiplier = 2.0

type resolvedRetry struct {
    maxAttempts       int
    initialDelay      time.Duration
    maxDelay          time.Duration
    backoffMultiplier float64
}

func resolveRetry(config RetryConfig) (resolvedRetry, error) {
    maxAttempts, maxAttemptsErr := bound.Resolve(config.MaxAttempts, retryMaxAttemptsRule)
    if nil != maxAttemptsErr {
        return resolvedRetry{}, maxAttemptsErr
    }

    initialDelay, initialDelayErr := bound.Resolve(config.InitialDelay, retryInitialDelayRule)
    if nil != initialDelayErr {
        return resolvedRetry{}, initialDelayErr
    }

    maxDelay, maxDelayErr := bound.Resolve(config.MaxDelay, retryMaxDelayRule)
    if nil != maxDelayErr {
        return resolvedRetry{}, maxDelayErr
    }

    backoffMultiplier := config.BackoffMultiplier
    if 0 == backoffMultiplier {
        backoffMultiplier = defaultBackoffMultiplier
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN multiplier through and poison the float-space growth of the backoff */
    if false == (backoffMultiplier >= 1) {
        return resolvedRetry{}, exception.NewError(
            "the backoff multiplier is below 1",
            exceptioncontract.Context{
                "field": "RetryConfig.BackoffMultiplier",
                "value": strconv.FormatFloat(config.BackoffMultiplier, 'g', -1, 64),
            },
            nil,
        )
    }

    return resolvedRetry{
        maxAttempts:       maxAttempts.Value,
        initialDelay:      initialDelay.Value,
        maxDelay:          maxDelay.Value,
        backoffMultiplier: backoffMultiplier,
    }, nil
}
