package mysql

import (
    "testing"
    "time"
)

func TestDefaultRetryConfig(t *testing.T) {
    retryConfig := DefaultRetryConfig()

    if 3 != retryConfig.MaxAttempts {
        t.Fatalf("expected the default of 3 max attempts, got %d", retryConfig.MaxAttempts)
    }
    if 500*time.Millisecond != retryConfig.InitialDelay {
        t.Fatalf("expected the default initial delay of 500ms, got %s", retryConfig.InitialDelay)
    }
    if 5*time.Second != retryConfig.MaxDelay {
        t.Fatalf("expected the default max delay of 5s, got %s", retryConfig.MaxDelay)
    }
    if 2.0 != retryConfig.BackoffMultiplier {
        t.Fatalf("expected the default backoff multiplier of 2.0, got %f", retryConfig.BackoffMultiplier)
    }
}

func TestNewRetryConfigStoresValues(t *testing.T) {
    retryConfig := NewRetryConfig(4, time.Millisecond, time.Second, 3.0)

    if 4 != retryConfig.MaxAttempts {
        t.Fatalf("expected 4 max attempts, got %d", retryConfig.MaxAttempts)
    }
    if time.Millisecond != retryConfig.InitialDelay {
        t.Fatalf("expected initial delay 1ms, got %s", retryConfig.InitialDelay)
    }
    if time.Second != retryConfig.MaxDelay {
        t.Fatalf("expected max delay 1s, got %s", retryConfig.MaxDelay)
    }
    if 3.0 != retryConfig.BackoffMultiplier {
        t.Fatalf("expected backoff multiplier 3.0, got %f", retryConfig.BackoffMultiplier)
    }
}
