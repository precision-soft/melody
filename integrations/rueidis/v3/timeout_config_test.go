package rueidis

import (
    "testing"
    "time"
)

func TestDefaultTimeoutConfig_Values(t *testing.T) {
    timeoutConfig := DefaultTimeoutConfig()

    if 3*time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("default connect timeout must be 3s, got %s", timeoutConfig.ConnectTimeout)
    }

    if 3*time.Second != timeoutConfig.CommandTimeout {
        t.Fatalf("default command timeout must be 3s, got %s", timeoutConfig.CommandTimeout)
    }
}
