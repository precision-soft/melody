package service

import (
    "context"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func TestDispatchCommitted_DropsTheEntriesUnderACanceledCaller(t *testing.T) {
    cacheInstance := melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(16), CleanupInterval: melodybound.Of(time.Minute), Clock: melodyclock.NewSystemClock()}), examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    defer cacheInstance.CloseWithContext(context.Background())

    if setErr := cacheInstance.Set(exampleTestRuntime(), CacheKeyCategoryList, "stale", melodybound.UnlimitedDuration(), melodycachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("prime: %v", setErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    dispatchCommitted(melodyruntime.WithContext(exampleTestRuntime(), canceledContext), &refusingDispatcher{}, cacheInstance, "category.created", nil, "category-1", CacheKeyCategoryList)

    if held, _ := cacheInstance.Has(exampleTestRuntime(), CacheKeyCategoryList); true == held {
        t.Fatalf("expected the entry dropped under the canceled caller")
    }
}
