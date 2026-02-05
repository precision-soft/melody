package pgsql

import "time"

func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		ConnectTimeout: 5 * time.Second,
	}
}

type TimeoutConfig struct {
	ConnectTimeout time.Duration
}
