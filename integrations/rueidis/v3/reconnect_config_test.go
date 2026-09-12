package rueidis

import (
    "math"
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

/* a factor below 1 (or NaN) would decay the resubscribe backoff toward zero and turn a redis outage into a reconnect storm, so the resolver keeps the default instead */
func TestResolveReconnectConfig_SubUnitFactorKeepsTheDefault(t *testing.T) {
    for _, factor := range []float64{0.5, 0, -1, math.NaN()} {
        resolved := resolveReconnectConfig(&ReconnectConfig{BackoffFactor: factor})
        if 2.0 != resolved.BackoffFactor {
            t.Fatalf("expected factor %v to fall back to the default 2.0, got %v", factor, resolved.BackoffFactor)
        }
    }

    resolved := resolveReconnectConfig(&ReconnectConfig{BackoffFactor: 1})
    if 1.0 != resolved.BackoffFactor {
        t.Fatalf("expected the boundary factor 1 to be accepted, got %v", resolved.BackoffFactor)
    }
}

/* an initial backoff above the cap would make the first resubscribe wait exceed the declared maximum, so the resolver clamps it onto the cap */
func TestResolveReconnectConfig_InitialAboveTheCapClampsOntoTheCap(t *testing.T) {
    resolved := resolveReconnectConfig(&ReconnectConfig{InitialBackoff: 5 * time.Minute, MaxBackoff: 30 * time.Second})

    if 30*time.Second != resolved.InitialBackoff {
        t.Fatalf("expected the initial backoff to clamp onto the 30s cap, got %s", resolved.InitialBackoff)
    }

    if 30*time.Second != resolved.MaxBackoff {
        t.Fatalf("expected the max backoff to stay 30s, got %s", resolved.MaxBackoff)
    }
}
