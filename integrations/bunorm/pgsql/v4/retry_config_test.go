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

    if 3 != resolved.maxAttempts || 500*time.Millisecond != resolved.initialDelay || 5*time.Second != resolved.maxDelay || 2.0 != resolved.backoffMultiplier {
        t.Fatalf("expected the default retry 3 / 500ms / 5s / 2, got %+v", resolved)
    }
}

func TestResolveRetry_KeepsTheConfiguredValues(t *testing.T) {
    resolved, resolveErr := resolveRetry(RetryConfig{
        MaxAttempts:       bound.Of(1),
        InitialDelay:      bound.Of(time.Millisecond),
        MaxDelay:          bound.Of(time.Second),
        BackoffMultiplier: 1,
    })
    if nil != resolveErr {
        t.Fatalf("expected the retry to resolve, got %v", resolveErr)
    }

    if 1 != resolved.maxAttempts || time.Millisecond != resolved.initialDelay || time.Second != resolved.maxDelay || 1 != resolved.backoffMultiplier {
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
        {field: "RetryConfig.InitialDelay", config: RetryConfig{InitialDelay: bound.Of(time.Microsecond)}},
        {field: "RetryConfig.MaxDelay", config: RetryConfig{MaxDelay: bound.UnlimitedDuration()}},
        {field: "RetryConfig.BackoffMultiplier", config: RetryConfig{BackoffMultiplier: 0.5}},
        {field: "RetryConfig.BackoffMultiplier", config: RetryConfig{BackoffMultiplier: -1}},
        {field: "RetryConfig.BackoffMultiplier", config: RetryConfig{BackoffMultiplier: math.NaN()}},
    } {
        _, resolveErr := resolveRetry(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}
