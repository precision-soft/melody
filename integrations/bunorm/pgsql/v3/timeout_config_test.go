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
    timeoutConfig := NewTimeoutConfig(time.Second)

    if time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected connect timeout 1s, got %s", timeoutConfig.ConnectTimeout)
    }

    zeroTimeoutConfig := NewTimeoutConfig(0)

    if 0 != zeroTimeoutConfig.ConnectTimeout {
        t.Fatalf("expected a zero connect timeout to be preserved, got %s", zeroTimeoutConfig.ConnectTimeout)
    }
}

/* the released one-duration constructor leaves the driver deadlines to the provider's defaults, never to the connect timeout */
func TestNewTimeoutConfig_LeavesTheDeadlinesToTheDefaults(t *testing.T) {
    resolved := (&Provider{timeoutConfig: NewTimeoutConfig(7 * time.Second)}).resolvedTimeoutConfig()

    if 7*time.Second != resolved.ConnectTimeout {
        t.Fatalf("expected connect timeout 7s, got %s", resolved.ConnectTimeout)
    }

    if 30*time.Second != resolved.ReadTimeout || 30*time.Second != resolved.WriteTimeout {
        t.Fatalf("expected the read and write deadlines to take the 30s defaults, got %s and %s", resolved.ReadTimeout, resolved.WriteTimeout)
    }
}

func TestNewTimeoutConfigWithDeadlines_SetsAllThree(t *testing.T) {
    timeoutConfig := NewTimeoutConfigWithDeadlines(time.Second, 2*time.Second, 3*time.Second)

    if time.Second != timeoutConfig.ConnectTimeout || 2*time.Second != timeoutConfig.ReadTimeout || 3*time.Second != timeoutConfig.WriteTimeout {
        t.Fatalf("expected the three durations stored, got %+v", timeoutConfig)
    }
}
