package rueidis

import (
    "context"
    "os"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    redisclient "github.com/redis/rueidis"
)

func newTokenStoreRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newTokenStoreClient(t *testing.T) redisclient.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis token store integration test")
    }

    provider := NewProvider()
    client, openErr := provider.Open(NewConnectionParams(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    t.Cleanup(func() {
        provider.Close(client)
    })

    return client
}
