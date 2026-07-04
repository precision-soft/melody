package rueidis

import (
    "time"
)

func DefaultRetryConfig() *RetryConfig {
    return &RetryConfig{
        MaxAttempts:       3,
        InitialDelay:      500 * time.Millisecond,
        MaxDelay:          5 * time.Second,
        BackoffMultiplier: 2.0,
    }
}

func NewRetryConfig(
    maxAttempts uint32,
    initialDelay time.Duration,
    maxDelay time.Duration,
    backoffMultiplier float64,
) *RetryConfig {
    return &RetryConfig{
        MaxAttempts:       maxAttempts,
        InitialDelay:      initialDelay,
        MaxDelay:          maxDelay,
        BackoffMultiplier: backoffMultiplier,
    }
}

/* RetryConfig bounds the initial-connection retry: the provider re-dials transient failures (a redis that
is not accepting connections yet) with capped exponential backoff, so a cold-start race does not hard-fail. */
type RetryConfig struct {
    MaxAttempts       uint32
    InitialDelay      time.Duration
    MaxDelay          time.Duration
    BackoffMultiplier float64
}
