package mysql

import (
    "testing"
    "time"
)

func TestDefaultTimeoutConfig(t *testing.T) {
    timeoutConfig := DefaultTimeoutConfig()

    if 10*time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected the default connect timeout of 10s, got %s", timeoutConfig.ConnectTimeout)
    }
    if 30*time.Second != timeoutConfig.ReadTimeout {
        t.Fatalf("expected the default read timeout of 30s, got %s", timeoutConfig.ReadTimeout)
    }
    if 30*time.Second != timeoutConfig.WriteTimeout {
        t.Fatalf("expected the default write timeout of 30s, got %s", timeoutConfig.WriteTimeout)
    }
}

func TestNewTimeoutConfigStoresValues(t *testing.T) {
    timeoutConfig := NewTimeoutConfig(time.Second, 2*time.Second, 3*time.Second)

    if time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected connect timeout 1s, got %s", timeoutConfig.ConnectTimeout)
    }
    if 2*time.Second != timeoutConfig.ReadTimeout {
        t.Fatalf("expected read timeout 2s, got %s", timeoutConfig.ReadTimeout)
    }
    if 3*time.Second != timeoutConfig.WriteTimeout {
        t.Fatalf("expected write timeout 3s, got %s", timeoutConfig.WriteTimeout)
    }
}

func TestDefaultPoolConfig(t *testing.T) {
    poolConfig := DefaultPoolConfig()

    if 25 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected the default of 25 max open connections, got %d", poolConfig.MaxOpenConnections)
    }
    if 5 != poolConfig.MaxIdleConnections {
        t.Fatalf("expected the default of 5 max idle connections, got %d", poolConfig.MaxIdleConnections)
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
