package pgsql

import "time"

func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		ConnectTimeout: 5 * time.Second,
	}
}

func NewTimeoutConfig(
	connectTimeout time.Duration,
) *TimeoutConfig {
	return &TimeoutConfig{
		ConnectTimeout: connectTimeout,
	}
}

type TimeoutConfig struct {
	ConnectTimeout time.Duration
}
