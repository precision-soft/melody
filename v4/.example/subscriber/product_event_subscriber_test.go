package subscriber

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/service"
    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

func TestProductEventSubscriber_DropsTheViewCounterOnDelete(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    if _, incrementErr := cacheInstance.Increment(exampleTestRuntime(), service.CacheKeyProductViews("prod-7"), 3, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the counter: %v", incrementErr)
    }
    if _, incrementErr := cacheInstance.Increment(exampleTestRuntime(), service.CacheKeyProductViews("prod-8"), 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the neighbour's counter: %v", incrementErr)
    }

    listener := NewProductEventSubscriber().SubscribedEvents()[event.EventProductDeleted][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.EventProductDeleted, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock().Now()))

    if held, _ := cacheInstance.Has(exampleTestRuntime(), service.CacheKeyProductViews("prod-7")); true == held {
        t.Fatalf("expected the deleted product's view counter dropped")
    }
    if held, _ := cacheInstance.Has(exampleTestRuntime(), service.CacheKeyProductViews("prod-8")); false == held {
        t.Fatalf("expected another product's view counter kept")
    }
}

func TestProductEventSubscriber_DeletedDropsTheDailyCounter(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    for _, productId := range []string{"prod-7", "prod-8"} {
        if _, incrementErr := cacheInstance.Increment(exampleTestRuntime(), service.CacheKeyProductDailyViews(productId), 2, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)}); nil != incrementErr {
            t.Fatalf("prime the window counter of %s: %v", productId, incrementErr)
        }
    }

    listener := NewProductEventSubscriber().SubscribedEvents()[event.EventProductDeleted][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.EventProductDeleted, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock().Now()))

    if held, _ := cacheInstance.Has(exampleTestRuntime(), service.CacheKeyProductDailyViews("prod-7")); true == held {
        t.Fatalf("expected the deleted product's window counter dropped")
    }
    if held, _ := cacheInstance.Has(exampleTestRuntime(), service.CacheKeyProductDailyViews("prod-8")); false == held {
        t.Fatalf("expected another product's window counter kept")
    }
}

func TestProductEventSubscriber_DropsTheEntriesUnderARequestContextAlreadyDone(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    if _, incrementErr := cacheInstance.Increment(exampleTestRuntime(), service.CacheKeyProductViews("prod-7"), 3, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("prime the counter: %v", incrementErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()
    canceledRuntime := melodyruntime.WithContext(runtimeInstance, canceledContext)

    listener := NewProductEventSubscriber().SubscribedEvents()[event.EventProductDeleted][0].Listener()
    _ = listener(canceledRuntime, melodyevent.NewEvent(event.EventProductDeleted, event.NewProductDeletedEvent("prod-7"), melodyclock.NewSystemClock().Now()))

    if held, _ := cacheInstance.Has(exampleTestRuntime(), service.CacheKeyProductViews("prod-7")); true == held {
        t.Fatalf("expected the view counter dropped although the request context was done")
    }
}

func TestProductEventSubscriber_BroadcastsOnTheCatalogueTopic(t *testing.T) {
    fixture := notificationUnderTest(t)

    listener := NewProductEventSubscriber().SubscribedEvents()[event.EventProductDeleted][0].Listener()
    if listenErr := listener(fixture.runtimeInstance, melodyevent.NewEvent(event.EventProductDeleted, event.NewProductDeletedEvent("product-7"), melodyclock.NewSystemClock().Now())); nil != listenErr {
        t.Fatalf("the listener failed: %v", listenErr)
    }

    notification := receivedNotification(t, fixture.catalogPage, "on the catalogue topic")
    if service.CatalogJournalSubjectProduct != notification.Subject || "product-7" != notification.SubjectId {
        t.Fatalf("unexpected notification %+v", notification)
    }

    assertNoNotification(t, fixture.userDirectoryPage, "on the user directory topic")
}
