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

        if 0 < general.BackoffFactor {
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

        if 0 < override.BackoffFactor {
            resolved.BackoffFactor = override.BackoffFactor
        }
    }

    return resolved
}
