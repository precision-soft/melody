package pgsql

import (
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
        {field: "RetryConfig.Backoff.Max", config: RetryConfig{Backoff: bound.Backoff{Max: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: 0.5}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: -1}}},
        {field: "RetryConfig.Backoff.Factor", config: RetryConfig{Backoff: bound.Backoff{Factor: math.NaN()}}},
    } {
        _, resolveErr := resolveRetry(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
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
