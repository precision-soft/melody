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
