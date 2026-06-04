package rueidis_test

import (
    "context"
    "os"
    "testing"
    "time"

    rueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestRedisLock_MutualExclusionReleaseAndRefresh(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis lock integration test")
    }

    provider := rueidis.NewProvider()
    client, openErr := provider.Open(rueidis.NewConnectionParams(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer provider.Close(client)

    locker := rueidis.NewLocker(client)
    runtimeInstance := newLockRuntime()

    name := "melody:lock:test"

    first := locker.CreateLock(name, 10*time.Second)
    second := locker.CreateLock(name, 10*time.Second)

    acquired, acquireErr := first.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected first acquire to succeed: %v %v", acquired, acquireErr)
    }

    contended, contendedErr := second.Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected contention while held: %v %v", contended, contendedErr)
    }

    if refreshErr := first.Refresh(runtimeInstance, 10*time.Second); nil != refreshErr {
        t.Fatalf("refresh: %v", refreshErr)
    }

    if releaseErr := first.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    afterRelease, afterReleaseErr := second.Acquire(runtimeInstance)
    if nil != afterReleaseErr || false == afterRelease {
        t.Fatalf("expected acquire after release: %v %v", afterRelease, afterReleaseErr)
    }

    _ = second.Release(runtimeInstance)
}
