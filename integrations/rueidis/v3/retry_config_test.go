package rueidis

import (
    "errors"
    "math"
    "testing"
    "time"
)

/* the default retry budget is a small, bounded backoff, not an unbounded loop. */
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

/* a cold-start dial failure (connection refused / no such host / timeout) is transient and retried; an unrelated error is not, so a real misconfiguration still fails fast. */
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

/* the backoff grows geometrically from the initial delay and is capped at the max delay. */
func TestComputeBackoffDelay(t *testing.T) {
    provider := &Provider{retryConfig: NewRetryConfig(10, time.Second, 5*time.Second, 2.0)}

    cases := map[uint32]time.Duration{
        1:  1 * time.Second,
        2:  2 * time.Second,
        3:  4 * time.Second,
        4:  5 * time.Second, /* 8s capped to 5s */
        9:  5 * time.Second, /* stays capped */
        40: 5 * time.Second, /* extreme attempt still caps — must not overflow float64->int64 to a negative delay */
    }

    for attempt, expected := range cases {
        delay := provider.computeBackoffDelay(attempt)
        if expected != delay {
            t.Fatalf("attempt %d: wanted %s, got %s", attempt, expected, delay)
        }
    }
}

/* negative delays and a sub-1 multiplier are degenerate (an immediate sleep, a decaying delay) and resolve to the defaults instead of collapsing the backoff. */
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

/* NaN fails every comparison, so a NaN multiplier would slip through a `1 > x` clamp, poison the float-space growth and convert to a negative duration — an immediate re-dial storm; the not-at-least-1 clamp resolves it to the default. */
func TestComputeBackoffDelayNaNMultiplierFallsBackToDefault(t *testing.T) {
    provider := &Provider{retryConfig: NewRetryConfig(3, -time.Second, -time.Second, math.NaN())}

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a NaN multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the NaN fallback to keep the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}
