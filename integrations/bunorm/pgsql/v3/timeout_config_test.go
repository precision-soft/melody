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

func TestNewTimeoutConfigStoresValuesIncludingZero(t *testing.T) {
    timeoutConfig := NewTimeoutConfig(time.Second, 0, 0)

    if time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected connect timeout 1s, got %s", timeoutConfig.ConnectTimeout)
    }

    zeroTimeoutConfig := NewTimeoutConfig(0, 0, 0)

    if 0 != zeroTimeoutConfig.ConnectTimeout {
        t.Fatalf("expected a zero connect timeout to be preserved, got %s", zeroTimeoutConfig.ConnectTimeout)
    }
}
