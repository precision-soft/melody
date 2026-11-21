package mysql

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* RetryConfig is the retry an open makes on a transient failure; its zero value is the default retry, and a single attempt is written as MaxAttempts bound.Of(1). */
type RetryConfig struct {
    /* MaxAttempts counts the attempts, the first one included: 3 by default, at least 1, unlimited refused, since an open retried for ever hides a dead database from the boot and from a read/write splitter. */
    MaxAttempts bound.Count

    /* Backoff is the wait between two attempts: 500ms before the second, doubled for every attempt after it, capped at 5s, each field taking that default when left out; a Factor below 1 is refused, since it decays the wait into a re-dial storm. */
    Backoff bound.Backoff
}

var retryMaxAttemptsRule = bound.Rule[int]{
    Name:    "RetryConfig.MaxAttempts",
    Default: 3,
    Minimum: 1,
}

var retryBackoffRule = bound.BackoffRule{
    Name:           "RetryConfig.Backoff",
    InitialDefault: 500 * time.Millisecond,
    MaxDefault:     5 * time.Second,
    FactorDefault:  2,
}

type resolvedRetry struct {
    maxAttempts int
    backoff     bound.ResolvedBackoff
}

func resolveRetry(config RetryConfig) (resolvedRetry, error) {
    maxAttempts, maxAttemptsErr := bound.Resolve(config.MaxAttempts, retryMaxAttemptsRule)
    if nil != maxAttemptsErr {
        return resolvedRetry{}, maxAttemptsErr
    }

    backoff, backoffErr := bound.ResolveBackoff(config.Backoff, retryBackoffRule)
    if nil != backoffErr {
        return resolvedRetry{}, backoffErr
    }

    return resolvedRetry{
        maxAttempts: maxAttempts.Value,
        backoff:     backoff,
    }, nil
}
