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
