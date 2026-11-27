package subscriber

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

func TestCategoryEventSubscriber_EveryListenerDropsItsEntriesUnderACanceledEvent(t *testing.T) {
    categoryInstance := entity.NewCategory("category-7", "Books")
    eventList := map[string]any{
        event.EventCategoryCreated: event.NewCategoryCreatedEvent(categoryInstance),
        event.EventCategoryUpdated: event.NewCategoryUpdatedEvent(categoryInstance),
        event.EventCategoryDeleted: event.NewCategoryDeletedEvent(categoryInstance.Id),
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

/* assertCategoryChangeHandled runs one listener and asserts what every category write owes: both entries dropped, one notification on the catalogue topic and one journal entry, each naming the action and the category */
func assertCategoryChangeHandled(t *testing.T, eventName string, payload any, action string) {
    t.Helper()

    fixture := notificationUnderTest(t)
    cacheInstance, page, journalRepository, runtimeInstance := fixture.cache, fixture.catalogPage, fixture.journalRepository, fixture.runtimeInstance

    keyList := []string{service.CacheKeyCategoryById("category-7"), service.CacheKeyCategoryList}
    for _, key := range keyList {
        if setErr := cacheInstance.Set(exampleTestRuntime(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }

    listener := NewCategoryEventSubscriber().SubscribedEvents()[eventName][0].Listener()
    if listenErr := listener(runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock().Now())); nil != listenErr {
        t.Fatalf("%s: the listener failed: %v", eventName, listenErr)
    }

    for _, key := range keyList {
        if held, _ := cacheInstance.Has(exampleTestRuntime(), key); true == held {
            t.Fatalf("%s: expected %s dropped", eventName, key)
        }
    }

    select {
    case delivered := <-page.Events():
        notification := catalogNotification{}
        if decodeErr := json.Unmarshal([]byte(delivered.Data), &notification); nil != decodeErr {
            t.Fatalf("%s: decode the notification %q: %v", eventName, delivered.Data, decodeErr)
        }

        if catalogNotificationEvent != delivered.Event || action != notification.Action || service.CatalogJournalSubjectCategory != notification.Subject || "category-7" != notification.SubjectId {
            t.Fatalf("%s: unexpected notification %+v %+v", eventName, delivered, notification)
        }
    default:
        t.Fatalf("%s: expected the catalogue topic notified", eventName)
    }

    assertNoNotification(t, fixture.userDirectoryPage, eventName+" on the user directory topic")

    entryList, latestErr := journalRepository.Latest(context.Background(), 10)
    if nil != latestErr {
        t.Fatalf("%s: read the journal: %v", eventName, latestErr)
    }

    if 1 != len(entryList) || action != entryList[0].Action || service.CatalogJournalSubjectCategory != entryList[0].Subject || "category-7" != entryList[0].SubjectId {
        t.Fatalf("%s: expected one journal entry for the change, got %+v", eventName, entryList)
    }
}

func TestCategoryEventSubscriber_CreatedDropsTheListAndNotifiesTheCatalogueTopic(t *testing.T) {
    assertCategoryChangeHandled(t, event.EventCategoryCreated, event.NewCategoryCreatedEvent(entity.NewCategory("category-7", "Books")), repository.CatalogJournalActionCreated)
}

func TestCategoryEventSubscriber_UpdatedDropsTheListAndNotifiesTheCatalogueTopic(t *testing.T) {
    assertCategoryChangeHandled(t, event.EventCategoryUpdated, event.NewCategoryUpdatedEvent(entity.NewCategory("category-7", "Novels")), repository.CatalogJournalActionUpdated)
}

func TestCategoryEventSubscriber_DeletedDropsTheListAndNotifiesTheCatalogueTopic(t *testing.T) {
    assertCategoryChangeHandled(t, event.EventCategoryDeleted, event.NewCategoryDeletedEvent("category-7"), repository.CatalogJournalActionDeleted)
}
