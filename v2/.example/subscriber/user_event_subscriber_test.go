package subscriber

import (
    "context"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/event"
    "github.com/precision-soft/melody/v2/.example/service"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
)

/* the helper is what lets one listener drop two spellings of one user — the previous and the current username of a rename — without either of them ever addressing the raw spelling the cache never held */
func TestUserUsernameCacheKeyFoldsTheSpelling(t *testing.T) {
    if expected := service.CacheKeyUserByUsername("café"); expected != userUsernameCacheKey(" Café ") {
        t.Fatalf("expected the folded key %q, got %q", expected, userUsernameCacheKey(" Café "))
    }
}

func TestUserUsernameCacheKeyAnswersEmptyForABlankUsername(t *testing.T) {
    if "" != userUsernameCacheKey("   ") {
        t.Fatalf("expected a blank username to answer no key, got %q", userUsernameCacheKey("   "))
    }
}

/* the listener drops its entries before it journals the change, so the catalogue journal the runtime does not hold ends the call, a panic recovered here, after the drops under test */
func dispatchUserUpdated(t *testing.T, cacheInstance melodycachecontract.Cache, user *entity.User, previousUsername string) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    defer func() {
        _ = recover()
    }()

    listener := NewUserEventSubscriber().SubscribedEvents()[event.UserUpdatedEventName][0].Listener()
    _ = listener(runtimeInstance, melodyevent.NewEvent(event.UserUpdatedEventName, event.NewUserUpdatedEvent(user, previousUsername), melodyclock.NewSystemClock()))
}

/* a rename leaves the by-username entry under the spelling before it; dropped, the name before the rename can neither sign in from the cache nor be refused as taken when it is registered again */
func TestUserEventSubscriber_DropsBothSpellingsOnARename(t *testing.T) {
    clockInstance := melodyclock.NewSystemClock()
    cacheInstance := melodycache.NewManagerOwningBackend(melodycache.NewInMemoryBackend(128, time.Minute, clockInstance), examplecache.NewGobSerializer())
    t.Cleanup(func() { _ = cacheInstance.Close() })

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

    dispatchUserUpdated(t, cacheInstance, entity.NewUser("user-7", "grace", "hash", []string{entity.RoleUser}), "ada")

    for _, key := range keyList {
        if held, _ := cacheInstance.Has(key); true == held {
            t.Fatalf("expected %s dropped after the rename", key)
        }
    }
}
