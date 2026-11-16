package subscriber

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/service"
    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
)

func TestProductEventSubscriber_DropsTheViewCounterOnDelete(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    if _, incrementErr := cacheInstance.Increment(context.Background(), service.CacheKeyProductViews("prod-7"), 3, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the counter: %v", incrementErr)
    }
    if _, incrementErr := cacheInstance.Increment(context.Background(), service.CacheKeyProductViews("prod-8"), 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the neighbour's counter: %v", incrementErr)
    }

    listener := NewProductEventSubscriber().SubscribedEvents()[event.ProductDeletedEventName][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.ProductDeletedEventName, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock()))

    if held, _ := cacheInstance.Has(context.Background(), service.CacheKeyProductViews("prod-7")); true == held {
        t.Fatalf("expected the deleted product's view counter dropped")
    }
    if held, _ := cacheInstance.Has(context.Background(), service.CacheKeyProductViews("prod-8")); false == held {
        t.Fatalf("expected another product's view counter kept")
    }
}

func TestProductEventSubscriber_DropsTheEntriesUnderARequestContextAlreadyDone(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    if _, incrementErr := cacheInstance.Increment(context.Background(), service.CacheKeyProductViews("prod-7"), 3, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the counter: %v", incrementErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()
    canceledRuntime := melodyruntime.New(canceledContext, runtimeInstance.Scope(), runtimeInstance.Container())

    listener := NewProductEventSubscriber().SubscribedEvents()[event.ProductDeletedEventName][0].Listener()
    _ = listener(canceledRuntime, melodyevent.NewEvent(event.ProductDeletedEventName, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock()))

    if held, _ := cacheInstance.Has(context.Background(), service.CacheKeyProductViews("prod-7")); true == held {
        t.Fatalf("expected the view counter dropped although the request context was done")
    }
}
