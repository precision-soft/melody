package subscriber

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
)

func TestProductEventSubscriber_DropsTheViewCounterOnDelete(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    if _, incrementErr := cacheInstance.Increment(service.CacheKeyProductViews("prod-7"), 3); nil != incrementErr {
        t.Fatalf("prime the counter: %v", incrementErr)
    }
    if _, incrementErr := cacheInstance.Increment(service.CacheKeyProductViews("prod-8"), 1); nil != incrementErr {
        t.Fatalf("prime the neighbour's counter: %v", incrementErr)
    }

    listener := NewProductEventSubscriber().SubscribedEvents()[event.ProductDeletedEventName][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.ProductDeletedEventName, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock()))

    if held, _ := cacheInstance.Has(service.CacheKeyProductViews("prod-7")); true == held {
        t.Fatalf("expected the deleted product's view counter dropped")
    }
    if held, _ := cacheInstance.Has(service.CacheKeyProductViews("prod-8")); false == held {
        t.Fatalf("expected another product's view counter kept")
    }
}
