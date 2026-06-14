package rueidis

import (
    "os"
    "testing"
    "time"
)

func TestProvider_Open_ZeroConnectTimeoutPingsWithoutDeadline(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    provider := NewProvider(
        WithTimeoutConfig(
            &TimeoutConfig{
                CommandTimeout: 3 * time.Second,
            },
        ),
    )

    client, openErr := provider.Open(NewConnectionParams(address, "", ""))
    if nil != openErr {
        t.Fatalf("open with zero connect timeout against healthy redis: %v", openErr)
    }

    closeErr := provider.Close(client)
    if nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}
