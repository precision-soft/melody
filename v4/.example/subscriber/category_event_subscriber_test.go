package subscriber

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

func TestCategoryEventSubscriber_EveryListenerDropsItsEntriesUnderACanceledEvent(t *testing.T) {
    categoryInstance := entity.NewCategory("category-7", "Books")
    eventList := map[string]any{
        event.CategoryCreatedEventName: event.NewCategoryCreatedEvent(categoryInstance),
        event.CategoryUpdatedEventName: event.NewCategoryUpdatedEvent(categoryInstance),
        event.CategoryDeletedEventName: event.NewCategoryDeletedEvent(categoryInstance.Id),
    }

    for eventName, payload := range eventList {
        cacheInstance, runtimeInstance := userUpdateUnderTest(t)

        keyList := []string{service.CacheKeyCategoryById(categoryInstance.Id), service.CacheKeyCategoryList}
        for _, key := range keyList {
            if setErr := cacheInstance.Set(exampleTestRuntime(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
                t.Fatalf("prime %s: %v", key, setErr)
            }
        }

        canceledContext, cancel := context.WithCancel(context.Background())
        cancel()

        listener := NewCategoryEventSubscriber().SubscribedEvents()[eventName][0].Listener()
        _ = listener(melodyruntime.WithContext(runtimeInstance, canceledContext), melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock().Now()))

        for _, key := range keyList {
            if held, _ := cacheInstance.Has(exampleTestRuntime(), key); true == held {
                t.Fatalf("%s: expected %s dropped under the canceled event", eventName, key)
            }
        }
    }
}
