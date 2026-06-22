package amqp

import (
    "testing"
    "time"
)

func TestDefaultReconnectConfig(t *testing.T) {
    config := DefaultReconnectConfig()

    if 1*time.Second != config.InitialBackoff {
        t.Fatalf("expected initial backoff 1s, got %s", config.InitialBackoff)
    }

    if 30*time.Second != config.MaxBackoff {
        t.Fatalf("expected max backoff 30s, got %s", config.MaxBackoff)
    }

    if 2.0 != config.BackoffFactor {
        t.Fatalf("expected backoff factor 2.0, got %v", config.BackoffFactor)
    }
}

func TestNewReconnectConfig(t *testing.T) {
    config := NewReconnectConfig(2*time.Second, time.Minute, 3.0)

    if 2*time.Second != config.InitialBackoff || time.Minute != config.MaxBackoff || 3.0 != config.BackoffFactor {
        t.Fatalf("unexpected reconnect config: %+v", config)
    }
}

func TestNextReconnectBackoff_ClampsZeroMaxBackoff(t *testing.T) {
    config := ReconnectConfig{InitialBackoff: time.Second, MaxBackoff: 0, BackoffFactor: 2}

    if next := nextReconnectBackoff(config, time.Second); 0 >= next {
        t.Fatalf("expected a positive backoff when MaxBackoff is non-positive, got %v", next)
    }

    /* @info a huge current still caps at the default max instead of returning the zero cap */
    if next := nextReconnectBackoff(config, time.Hour); DefaultReconnectConfig().MaxBackoff != next {
        t.Fatalf("expected the zero MaxBackoff to fall back to the default cap, got %v", next)
    }
}

func TestNextReconnectBackoff_ClampsOverflowingProduct(t *testing.T) {
    /* @info a BackoffFactor large enough to push the float product past the int64 nanosecond range used to wrap the time.Duration conversion negative, skip the cap, and feed time.After a no-delay backoff; the result must stay clamped to MaxBackoff */
    config := ReconnectConfig{InitialBackoff: time.Second, MaxBackoff: 30 * time.Second, BackoffFactor: 1e10}

    next := nextReconnectBackoff(config, time.Second)

    if 0 >= next {
        t.Fatalf("expected a positive backoff, got %v (overflow defeated the cap)", next)
    }

    if next > config.MaxBackoff {
        t.Fatalf("expected the backoff clamped to MaxBackoff %s, got %v", config.MaxBackoff, next)
    }
}

func TestReconnectBackoffShouldReset_ClampsZeroInitialBackoff(t *testing.T) {
    /* @info a directly-constructed config with a zero InitialBackoff must measure the reset threshold against the clamped default, not the raw 0: a 0 threshold would reset on every instantly-dying subscription and defeat the no-delay-storm guard the seed/reset sites already clamp for */
    config := ReconnectConfig{InitialBackoff: 0, MaxBackoff: time.Second, BackoffFactor: 2}

    if true == reconnectBackoffShouldReset(config, 0) {
        t.Fatalf("expected a zero-lived subscription not to reset the backoff under the clamped default threshold")
    }

    if true == reconnectBackoffShouldReset(config, clampedInitialBackoff(config)-1) {
        t.Fatalf("expected a sub-threshold subscription not to reset the backoff")
    }

    if false == reconnectBackoffShouldReset(config, clampedInitialBackoff(config)) {
        t.Fatalf("expected a subscription that lived at least the clamped initial backoff to reset")
    }
}

func TestResolveReconnectConfig_NilNilFallsBackToDefault(t *testing.T) {
    resolved := resolveReconnectConfig(nil, nil)
    defaults := DefaultReconnectConfig()

    if defaults.InitialBackoff != resolved.InitialBackoff || defaults.MaxBackoff != resolved.MaxBackoff || defaults.BackoffFactor != resolved.BackoffFactor {
        t.Fatalf("expected default config, got %+v", resolved)
    }
}

func TestResolveReconnectConfig_GeneralOverridesDefault(t *testing.T) {
    general := &ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute}

    resolved := resolveReconnectConfig(general, nil)

    if 2*time.Second != resolved.InitialBackoff {
        t.Fatalf("expected general initial backoff 2s, got %s", resolved.InitialBackoff)
    }

    if time.Minute != resolved.MaxBackoff {
        t.Fatalf("expected general max backoff 1m, got %s", resolved.MaxBackoff)
    }

    if 2.0 != resolved.BackoffFactor {
        t.Fatalf("expected default backoff factor 2.0, got %v", resolved.BackoffFactor)
    }
}

func TestResolveReconnectConfig_OverrideWinsPerField(t *testing.T) {
    general := &ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute, BackoffFactor: 4.0}
    override := &ReconnectConfig{MaxBackoff: 10 * time.Second}

    resolved := resolveReconnectConfig(general, override)

    if 2*time.Second != resolved.InitialBackoff {
        t.Fatalf("expected inherited initial backoff 2s, got %s", resolved.InitialBackoff)
    }

    if 10*time.Second != resolved.MaxBackoff {
        t.Fatalf("expected overridden max backoff 10s, got %s", resolved.MaxBackoff)
    }

    if 4.0 != resolved.BackoffFactor {
        t.Fatalf("expected inherited backoff factor 4.0, got %v", resolved.BackoffFactor)
    }
}
