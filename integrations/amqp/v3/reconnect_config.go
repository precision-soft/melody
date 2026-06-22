package amqp

import "time"

func DefaultReconnectConfig() *ReconnectConfig {
    return &ReconnectConfig{
        InitialBackoff: 1 * time.Second,
        MaxBackoff:     30 * time.Second,
        BackoffFactor:  2.0,
    }
}

func NewReconnectConfig(
    initialBackoff time.Duration,
    maxBackoff time.Duration,
    backoffFactor float64,
) *ReconnectConfig {
    return &ReconnectConfig{
        InitialBackoff: initialBackoff,
        MaxBackoff:     maxBackoff,
        BackoffFactor:  backoffFactor,
    }
}

type ReconnectConfig struct {
    InitialBackoff time.Duration
    MaxBackoff     time.Duration
    BackoffFactor  float64
}

func clampedInitialBackoff(config ReconnectConfig) time.Duration {
    if 0 >= config.InitialBackoff {
        return DefaultReconnectConfig().InitialBackoff
    }

    return config.InitialBackoff
}

func clampedMaxBackoff(config ReconnectConfig) time.Duration {
    if 0 >= config.MaxBackoff {
        return DefaultReconnectConfig().MaxBackoff
    }

    return config.MaxBackoff
}

func nextReconnectBackoff(config ReconnectConfig, current time.Duration) time.Duration {
    maxBackoff := clampedMaxBackoff(config)

    next := time.Duration(float64(current) * config.BackoffFactor)
    if next > maxBackoff {
        return maxBackoff
    }

    return next
}

/* @important only treat a subscription as healthy enough to reset the backoff when it actually lived at least the initial backoff: a subscribe that succeeds but loses its channel immediately must keep backing off, otherwise it becomes a no-delay reconnect storm against the broker. */
func reconnectBackoffShouldReset(config ReconnectConfig, subscriptionDuration time.Duration) bool {
    return config.InitialBackoff <= subscriptionDuration
}

func resolveReconnectConfig(general *ReconnectConfig, override *ReconnectConfig) ReconnectConfig {
    defaults := DefaultReconnectConfig()

    resolved := ReconnectConfig{
        InitialBackoff: defaults.InitialBackoff,
        MaxBackoff:     defaults.MaxBackoff,
        BackoffFactor:  defaults.BackoffFactor,
    }

    if nil != general {
        if 0 < general.InitialBackoff {
            resolved.InitialBackoff = general.InitialBackoff
        }

        if 0 < general.MaxBackoff {
            resolved.MaxBackoff = general.MaxBackoff
        }

        if 1 <= general.BackoffFactor {
            resolved.BackoffFactor = general.BackoffFactor
        }
    }

    if nil != override {
        if 0 < override.InitialBackoff {
            resolved.InitialBackoff = override.InitialBackoff
        }

        if 0 < override.MaxBackoff {
            resolved.MaxBackoff = override.MaxBackoff
        }

        if 1 <= override.BackoffFactor {
            resolved.BackoffFactor = override.BackoffFactor
        }
    }

    return resolved
}
