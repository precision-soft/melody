package rueidis

import (
    "time"
)

func DefaultTimeoutConfig() *TimeoutConfig {
    return &TimeoutConfig{
        ConnectTimeout: 3 * time.Second,
        CommandTimeout: 3 * time.Second,
    }
}

type TimeoutConfig struct {
    ConnectTimeout time.Duration
    CommandTimeout time.Duration
}
