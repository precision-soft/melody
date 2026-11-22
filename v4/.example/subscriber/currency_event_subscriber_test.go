package subscriber

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

func TestCurrencyEventSubscriber_EveryListenerDropsItsEntriesUnderACanceledEvent(t *testing.T) {
    currencyInstance := entity.NewCurrency("currency-7", "EUR", "Euro", 1, time.Unix(0, 0))
    eventList := map[string]any{
        event.EventCurrencyCreated: event.NewCurrencyCreatedEvent(currencyInstance),
        event.EventCurrencyUpdated: event.NewCurrencyUpdatedEvent(currencyInstance),
        event.EventCurrencyDeleted: event.NewCurrencyDeletedEvent(currencyInstance.Id),
    }

    for eventName, payload := range eventList {
        cacheInstance, runtimeInstance := userUpdateUnderTest(t)

        keyList := []string{service.CacheKeyCurrencyById(currencyInstance.Id), service.CacheKeyCurrencyList}
        for _, key := range keyList {
            if setErr := cacheInstance.Set(exampleTestRuntime(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
                t.Fatalf("prime %s: %v", key, setErr)
            }
        }

        canceledContext, cancel := context.WithCancel(context.Background())
        cancel()

        listener := NewCurrencyEventSubscriber().SubscribedEvents()[eventName][0].Listener()
        _ = listener(melodyruntime.WithContext(runtimeInstance, canceledContext), melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock().Now()))

        for _, key := range keyList {
            if held, _ := cacheInstance.Has(exampleTestRuntime(), key); true == held {
                t.Fatalf("%s: expected %s dropped under the canceled event", eventName, key)
            }
        }
    }
}
