package rueidis

import (
    "testing"
    "time"
)

func TestDefaultReconnectConfig(t *testing.T) {
    config := DefaultReconnectConfig()

    if 1*time.Second != config.InitialBackoff || 30*time.Second != config.MaxBackoff || 2.0 != config.BackoffFactor {
        t.Fatalf("unexpected default reconnect config: %+v", config)
    }
}

func TestResolveReconnectConfig_NilFallsBackToDefault(t *testing.T) {
    resolved := resolveReconnectConfig(nil)
    defaults := DefaultReconnectConfig()

    if defaults.InitialBackoff != resolved.InitialBackoff || defaults.MaxBackoff != resolved.MaxBackoff || defaults.BackoffFactor != resolved.BackoffFactor {
        t.Fatalf("expected default config, got %+v", resolved)
    }
}

func TestResolveReconnectConfig_OverrideWinsPerField(t *testing.T) {
    resolved := resolveReconnectConfig(&ReconnectConfig{MaxBackoff: 10 * time.Second})

    if 1*time.Second != resolved.InitialBackoff {
        t.Fatalf("expected inherited initial backoff 1s, got %s", resolved.InitialBackoff)
    }

    if 10*time.Second != resolved.MaxBackoff {
        t.Fatalf("expected overridden max backoff 10s, got %s", resolved.MaxBackoff)
    }

    if 2.0 != resolved.BackoffFactor {
        t.Fatalf("expected inherited backoff factor 2.0, got %v", resolved.BackoffFactor)
    }
}

func TestNextServerSentEventBackplaneBackoff_GrowsAndCaps(t *testing.T) {
    instance := &ServerSentEventBackplane{reconnect: resolveReconnectConfig(&ReconnectConfig{InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, BackoffFactor: 2.0})}

    expected := []time.Duration{2 * time.Second, 4 * time.Second, 4 * time.Second}

    current := instance.reconnect.InitialBackoff
    for index, want := range expected {
        current = instance.nextServerSentEventBackplaneBackoff(current)
        if want != current {
            t.Fatalf("step %d: expected %s, got %s", index, want, current)
        }
    }
}
