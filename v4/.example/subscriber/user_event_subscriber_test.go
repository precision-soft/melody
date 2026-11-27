package subscriber

import (
    "context"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
)

/* deleteCountingCache counts the deletions under each key over a working cache */
type deleteCountingCache struct {
    melodycachecontract.Cache
    mutex   sync.Mutex
    deletes map[string]int
}

func (instance *deleteCountingCache) Delete(runtimeInstance melodycontainercontract.Runtime, key string) error {
    instance.mutex.Lock()
    instance.deletes[key]++
    instance.mutex.Unlock()

    return instance.Cache.Delete(runtimeInstance, key)
}

/* userUpdateUnderTest answers a runtime holding only the cache: the listener drops its entries before it notifies, so the notification hub it cannot resolve ends the call after the drops under test */
func userUpdateUnderTest(t *testing.T) (*deleteCountingCache, melodycontainercontract.Runtime) {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    manager := melodycache.NewManager(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance}), examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})
    cacheInstance := &deleteCountingCache{Cache: manager, deletes: map[string]int{}}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return cacheInstance, melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
}

func dispatchUserUpdated(runtimeInstance melodycontainercontract.Runtime, user *entity.User, previousUsername string) {
    listener := NewUserEventSubscriber().SubscribedEvents()[event.EventUserUpdated][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.EventUserUpdated, event.NewUserUpdatedEvent(user, previousUsername), melodyclock.NewSystemClock().Now()))
}

/* a rename leaves the by-username entry under the spelling before it; dropped, the name before the rename can neither sign in from the cache nor be refused as taken when it is registered again */
func TestUserEventSubscriber_DropsBothSpellingsOnARename(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    renamed := entity.NewUser("user-7", "grace", "hash", []string{entity.RoleUser})
    keyList := []string{
        service.CacheKeyUserByUsername("ada"),
        service.CacheKeyUserByUsername("grace"),
        service.CacheKeyUserById("user-7"),
        service.CacheKeyUserList,
    }
    for _, key := range keyList {
        if setErr := cacheInstance.Set(exampleTestRuntime(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }

    dispatchUserUpdated(runtimeInstance, renamed, "ada")

    for _, key := range keyList {
        if held, _ := cacheInstance.Has(exampleTestRuntime(), key); true == held {
            t.Fatalf("expected %s dropped after the rename", key)
        }
    }
}

func TestUserEventSubscriber_DeletesTheUsernameOnceWhenTheNameIsKept(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    dispatchUserUpdated(runtimeInstance, entity.NewUser("user-7", "grace", "hash", []string{entity.RoleUser}), "grace")

    if deletes := cacheInstance.deletes[service.CacheKeyUserByUsername("grace")]; 1 != deletes {
        t.Fatalf("expected the kept username dropped once, got %d", deletes)
    }
}

func TestUserEventSubscriber_DropsTheEntriesUnderACanceledEvent(t *testing.T) {
    cacheInstance, runtimeInstance := userUpdateUnderTest(t)

    keyList := []string{service.CacheKeyUserById("user-7"), service.CacheKeyUserList}
    for _, key := range keyList {
        if setErr := cacheInstance.Set(exampleTestRuntime(), key, "stale", melodybound.UnlimitedDuration()); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    dispatchUserUpdated(melodyruntime.WithContext(runtimeInstance, canceledContext), entity.NewUser("user-7", "grace", "hash", []string{entity.RoleUser}), "grace")

    for _, key := range keyList {
        if held, _ := cacheInstance.Has(exampleTestRuntime(), key); true == held {
            t.Fatalf("expected %s dropped under the canceled event", key)
        }
    }
}

/* every account write is announced on the user directory topic, read behind the administrator's role, and never on the catalogue topic an editor reads */
func TestUserEventSubscriber_BroadcastsOnTheUserDirectoryTopicAlone(t *testing.T) {
    user := entity.NewUser("user-7", "grace", "hash", []string{entity.RoleUser})
    eventList := map[string]any{
        event.EventUserCreated: event.NewUserCreatedEvent(user),
        event.EventUserUpdated: event.NewUserUpdatedEvent(user, "grace"),
        event.EventUserDeleted: event.NewUserDeletedEvent("user-7", "grace"),
    }

    for eventName, payload := range eventList {
        fixture := notificationUnderTest(t)

        listener := NewUserEventSubscriber().SubscribedEvents()[eventName][0].Listener()
        if listenErr := listener(fixture.runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock().Now())); nil != listenErr {
            t.Fatalf("%s: the listener failed: %v", eventName, listenErr)
        }

        notification := receivedNotification(t, fixture.userDirectoryPage, eventName+" on the user directory topic")
        if service.CatalogJournalSubjectUser != notification.Subject || "user-7" != notification.SubjectId {
            t.Fatalf("%s: unexpected notification %+v", eventName, notification)
        }

        assertNoNotification(t, fixture.catalogPage, eventName+" on the catalogue topic")
    }
}
