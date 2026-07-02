package pgsql

import (
    "testing"
    "time"
)

func TestDefaultTimeoutConfig(t *testing.T) {
    timeoutConfig := DefaultTimeoutConfig()

    if 5*time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected the default connect timeout of 5s, got %s", timeoutConfig.ConnectTimeout)
    }
}

/* @info a zero ConnectTimeout is a valid configuration and must be preserved (the ping guard skips the deadline instead of using context.WithTimeout(0)) */

func TestNewTimeoutConfigStoresValuesIncludingZero(t *testing.T) {
    timeoutConfig := NewTimeoutConfig(time.Second)

    if time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected connect timeout 1s, got %s", timeoutConfig.ConnectTimeout)
    }

    zeroTimeoutConfig := NewTimeoutConfig(0)

    if 0 != zeroTimeoutConfig.ConnectTimeout {
        t.Fatalf("expected a zero connect timeout to be preserved, got %s", zeroTimeoutConfig.ConnectTimeout)
    }
}

func TestDefaultPoolConfig(t *testing.T) {
    poolConfig := DefaultPoolConfig()

    if 50 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected the default of 50 max open connections, got %d", poolConfig.MaxOpenConnections)
    }
    if 25 != poolConfig.MaxIdleConnections {
        t.Fatalf("expected the default of 25 max idle connections, got %d", poolConfig.MaxIdleConnections)
    }
    if 5*time.Minute != poolConfig.ConnectionMaxLifetime {
        t.Fatalf("expected the default connection max lifetime of 5m, got %s", poolConfig.ConnectionMaxLifetime)
    }
    if 1*time.Minute != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected the default connection max idle time of 1m, got %s", poolConfig.ConnectionMaxIdleTime)
    }
}

func TestNewPoolConfigStoresValues(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)

    if 10 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected 10 max open connections, got %d", poolConfig.MaxOpenConnections)
    }
    if 2 != poolConfig.MaxIdleConnections {
        t.Fatalf("expected 2 max idle connections, got %d", poolConfig.MaxIdleConnections)
    }
    if time.Minute != poolConfig.ConnectionMaxLifetime {
        t.Fatalf("expected connection max lifetime 1m, got %s", poolConfig.ConnectionMaxLifetime)
    }
    if time.Second != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected connection max idle time 1s, got %s", poolConfig.ConnectionMaxIdleTime)
    }
}

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
