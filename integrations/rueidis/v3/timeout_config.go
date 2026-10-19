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

/* TimeoutConfig bounds the provider's own ping round trips, at boot and through Ping, and nothing else: neither field reaches rueidis.ClientOption, so an ordinary command is bounded by ClientConfig.ConnWriteTimeout when its context carries no deadline, and a read-only one is retried past that for as long as its context allows. The budget that ends a command is the per-call timeout of the service issuing it. */
type TimeoutConfig struct {
    ConnectTimeout time.Duration
    CommandTimeout time.Duration
}
