package rueidis

import (
    "errors"
    "math"
    "testing"
    "time"
)

func TestDefaultRetryConfig(t *testing.T) {
    config := DefaultRetryConfig()

    if 3 != config.MaxAttempts {
        t.Fatalf("wanted 3 attempts, got %d", config.MaxAttempts)
    }
    if 500*time.Millisecond != config.InitialDelay {
        t.Fatalf("wanted 500ms initial delay, got %s", config.InitialDelay)
    }
    if 5*time.Second != config.MaxDelay {
        t.Fatalf("wanted 5s max delay, got %s", config.MaxDelay)
    }
    if 2.0 != config.BackoffMultiplier {
        t.Fatalf("wanted 2.0 multiplier, got %v", config.BackoffMultiplier)
    }
}

func TestIsTransientError(t *testing.T) {
    provider := &Provider{retryConfig: DefaultRetryConfig()}

    transient := []string{
        "dial tcp: connect: connection refused",
        "dial tcp: lookup redis: no such host",
        "i/o timeout",
        "network is unreachable",
        "EOF",
        "read tcp: use of closed network connection",
        "read: connection reset by peer",
        "write tcp: software caused connection abort",
        "write tcp: An established connection was aborted by the software in your host machine.",
        "LOADING Redis is loading the dataset in memory",
    }
    for _, message := range transient {
        if false == provider.isTransientError(errors.New(message)) {
            t.Fatalf("expected %q to be transient", message)
        }
    }

    if true == provider.isTransientError(errors.New("WRONGPASS invalid username-password pair")) {
        t.Fatalf("expected an auth error to be non-transient")
    }
    if true == provider.isTransientError(nil) {
        t.Fatalf("nil is not a transient error")
    }
}

func TestComputeBackoffDelay(t *testing.T) {
    provider := &Provider{retryConfig: NewRetryConfig(10, time.Second, 5*time.Second, 2.0)}

    cases := map[uint32]time.Duration{
        1:  1 * time.Second,
        2:  2 * time.Second,
        3:  4 * time.Second,
        4:  5 * time.Second,
        9:  5 * time.Second,
        40: 5 * time.Second,
    }

    for attempt, expected := range cases {
        delay := provider.computeBackoffDelay(attempt)
        if expected != delay {
            t.Fatalf("attempt %d: wanted %s, got %s", attempt, expected, delay)
        }
    }
}

func TestComputeBackoffDelayDegenerateValuesFallBackToDefaults(t *testing.T) {
    provider := &Provider{retryConfig: NewRetryConfig(3, -time.Second, -time.Second, 0.5)}

    if 500*time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected a negative initial delay to fall back to the default 500ms, got %s", provider.computeBackoffDelay(1))
    }

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a 0.5 multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected a negative max delay to fall back to the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}

func TestComputeBackoffDelayNaNMultiplierFallsBackToDefault(t *testing.T) {
    provider := &Provider{retryConfig: NewRetryConfig(3, -time.Second, -time.Second, math.NaN())}

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a NaN multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the NaN fallback to keep the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}
