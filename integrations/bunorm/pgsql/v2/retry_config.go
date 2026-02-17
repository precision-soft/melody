package pgsql

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

type RetryConfig struct {
	MaxAttempts       uint32
	InitialDelay      time.Duration
	MaxDelay          time.Duration
	BackoffMultiplier float64
}
