package rueidis

import (
    "errors"
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveRetry_AnAbsentFieldTakesItsDefault(t *testing.T) {
    resolved, resolveErr := resolveRetry(RetryConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero retry to resolve, got %v", resolveErr)
    }

    if 3 != resolved.maxAttempts || (bound.ResolvedBackoff{Initial: 500 * time.Millisecond, Max: 5 * time.Second, Factor: 2}) != resolved.backoff {
        t.Fatalf("expected the default retry 3 / 500ms / 5s / 2, got %+v", resolved)
    }
}

func TestResolveRetry_KeepsTheConfiguredValues(t *testing.T) {
    resolved, resolveErr := resolveRetry(RetryConfig{
        MaxAttempts: bound.Of(1),
        Backoff:     bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(time.Second), Factor: 1},
    })
    if nil != resolveErr {
        t.Fatalf("expected the retry to resolve, got %v", resolveErr)
    }

    if 1 != resolved.maxAttempts || (bound.ResolvedBackoff{Initial: time.Millisecond, Max: time.Second, Factor: 1}) != resolved.backoff {
        t.Fatalf("expected a single attempt over a constant backoff, got %+v", resolved)
    }
}

func TestResolveRetry_RefusesAFieldOutsideItsRuleNamingIt(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config RetryConfig
    }{
        {field: "RetryConfig.MaxAttempts", config: RetryConfig{MaxAttempts: bound.Of(0)}},
        {field: "RetryConfig.MaxAttempts", config: RetryConfig{MaxAttempts: bound.UnlimitedCount()}},
        {field: "RetryConfig.Backoff.Initial", config: RetryConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Microsecond)}}},
        {field: "RetryConfig.Backoff.Initial", config: RetryConfig{Backoff: bound.Backoff{Initial: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.Backoff.Max", config: RetryConfig{Backoff: bound.Backoff{Max: bound.Of(time.Microsecond)}}},
        {field: "RetryConfig.Backoff.Max", config: RetryConfig{Backoff: bound.Backoff{Max: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: 0.5}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: -1}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: math.NaN()}}},
    } {
        _, resolveErr := resolveRetry(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}

/* a cold-start dial failure (connection refused / no such host / timeout) is transient and retried; an unrelated error is not, so a real misconfiguration still fails fast. */
func TestIsTransientError(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

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
    provider := NewProvider(ProviderConfig{
        Retry: RetryConfig{MaxAttempts: bound.Of(10), Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}},
    })

    cases := map[int]time.Duration{
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

func TestComputeBackoffDelay_AbsentValuesTakeTheDefaults(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    if 500*time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected the default initial delay of 500ms, got %s", provider.computeBackoffDelay(1))
    }

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected the default multiplier of 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the default max delay clamp of 5s, got %s", provider.computeBackoffDelay(10))
    }
}

func TestResolveRetry_TheDefaultWaitsDoubleFromHalfASecondToTheCap(t *testing.T) {
    resolved, resolveErr := resolveRetry(RetryConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    expected := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
    for index, want := range expected {
        if delay := resolved.backoff.Delay(index + 1); want != delay {
            t.Fatalf("attempt %d: expected %s, got %s", index+1, want, delay)
        }
    }
}
