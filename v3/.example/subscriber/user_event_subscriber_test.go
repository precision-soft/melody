package subscriber

import (
    "context"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* deleteCountingCache counts the deletions under each key over a working cache */
type deleteCountingCache struct {
    melodycachecontract.Cache
    mutex   sync.Mutex
    deletes map[string]int
}

func (instance *deleteCountingCache) Delete(key string) error {
    instance.mutex.Lock()
    instance.deletes[key]++
    instance.mutex.Unlock()

    return instance.Cache.Delete(key)
}

/* userUpdateUnderTest answers a runtime holding only the cache: the listener drops its entries before it notifies, so the notification hub it cannot resolve ends the call after the drops under test */
func userUpdateUnderTest(t *testing.T) (*deleteCountingCache, melodyruntimecontract.Runtime) {
    t.Helper()

    clockInstance := melodyclock.NewSystemClock()
    manager := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    cacheInstance := &deleteCountingCache{Cache: manager, deletes: map[string]int{}}

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )

    return cacheInstance, melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func dispatchUserUpdated(runtimeInstance melodyruntimecontract.Runtime, user *entity.User, previousUsername string) {
    listener := NewUserEventSubscriber().SubscribedEvents()[event.UserUpdatedEventName][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.UserUpdatedEventName, event.NewUserUpdatedEvent(user, previousUsername), melodyclock.NewSystemClock()))
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
        if setErr := cacheInstance.Set(key, "stale", 0); nil != setErr {
            t.Fatalf("prime %s: %v", key, setErr)
        }
    }

    dispatchUserUpdated(runtimeInstance, renamed, "ada")

    for _, key := range keyList {
        if held, _ := cacheInstance.Has(key); true == held {
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
