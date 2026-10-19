package rueidis

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

func resolveReconnectConfig(override *ReconnectConfig) ReconnectConfig {
    defaults := DefaultReconnectConfig()

    resolved := ReconnectConfig{
        InitialBackoff: defaults.InitialBackoff,
        MaxBackoff:     defaults.MaxBackoff,
        BackoffFactor:  defaults.BackoffFactor,
    }

    if nil != override {
        if 0 < override.InitialBackoff {
            resolved.InitialBackoff = override.InitialBackoff
        }

        if 0 < override.MaxBackoff {
            resolved.MaxBackoff = override.MaxBackoff
        }

        /* a factor below 1 (or NaN) would decay the backoff toward zero and turn an outage into a reconnect storm; keep the default instead. */
        if 1 <= override.BackoffFactor {
            resolved.BackoffFactor = override.BackoffFactor
        }
    }

    /* an initial backoff above the cap would make the first wait exceed the declared maximum; clamp it onto the cap. */
    if resolved.MaxBackoff < resolved.InitialBackoff {
        resolved.InitialBackoff = resolved.MaxBackoff
    }

    return resolved
}
