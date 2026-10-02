package subscriber

import (
    "context"
    "errors"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* deletionRuntime is a runtime over a container holding the cache the user listener drops entries from and a silent journal */
func deletionRuntime(t *testing.T, cacheInstance melodycachecontract.Cache) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodycache.ServiceCache, func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
        return cacheInstance, nil
    })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    })

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

/* the three listeners of the deletion, registered in the composition root's order: the cache's first, the token release, then the two-factor release over the given store source */
func deletionDispatcher(tokenStore melodysecuritycontract.EpochRevocableTokenStore, twoFactorStore twofactor.StoreSource) *melodyevent.EventDispatcher {
    dispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
    dispatcher.AddSubscriber(NewUserEventSubscriber())
    dispatcher.AddSubscriber(NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return tokenStore
    }))
    dispatcher.AddSubscriber(NewTwoFactorEnrollmentSubscriber(twoFactorStore))

    return dispatcher
}

/* the token release outranks every other listener of the deletion, read off the real subscribers: the dispatcher ends a dispatch at the first listener that fails, and the release is the one that ends a deleted account's access */
func TestUserDeleted_TheTokenReleaseOutranksTheOtherListeners(t *testing.T) {
    releasePriority := NewAccessTokenReleaseSubscriber(nil).SubscribedEvents()[event.UserDeletedEventName][0].Priority()
    twoFactorPriority := NewTwoFactorEnrollmentSubscriber(nil).SubscribedEvents()[event.UserDeletedEventName][0].Priority()
    cachePriority := NewUserEventSubscriber().SubscribedEvents()[event.UserDeletedEventName][0].Priority()

    if releasePriority <= twoFactorPriority || twoFactorPriority <= cachePriority {
        t.Fatalf("expected the token release above the two-factor release above the cache listener, got %d, %d, %d", releasePriority, twoFactorPriority, cachePriority)
    }
}

/* the two-factor release meeting a store it cannot reach and the cache refusing its deletes, the deleted account's device token is still gone and its account revoked; the dispatch fails on the cache, which runs last. The control, the two releases over healthy stores, answers nil with the token and the enrollment released. */
func TestUserDeleted_AFailingListenerDoesNotLeaveTheDeletedAccountsTokens(t *testing.T) {
    clockInstance := melodyclock.NewSystemClock()
    backend := melodycache.NewInMemoryBackend(128, time.Minute, clockInstance)
    refusing := &refusingCache{Cache: melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())}

    tokenStore := melodysecurity.NewInMemoryTokenStore()
    tokenStore.Put("token-user-4", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"})

    unreachable := func(runtimeInstance melodyruntimecontract.Runtime) (*twofactor.Store, error) {
        return nil, errors.New("the catalogue database refused the migration")
    }

    runtimeInstance := deletionRuntime(t, refusing)
    _, dispatchErr := deletionDispatcher(tokenStore, unreachable).DispatchName(runtimeInstance, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))
    if nil == dispatchErr {
        t.Fatal("expected the dispatch to fail on the refusing cache")
    }

    if _, found, _ := tokenStore.Lookup(runtimeInstance, "token-user-4"); true == found {
        t.Fatal("expected the deleted account's device token released ahead of the listeners that failed")
    }

    if epoch, _ := tokenStore.RevocationEpoch(runtimeInstance, "user-4", ""); true == epoch.IsZero() {
        t.Fatal("expected the deleted account revoked ahead of the listeners that failed")
    }

    controlStore := melodysecurity.NewInMemoryTokenStore()
    controlStore.Put("token-user-4", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"})
    database, recorder := newRecordingDatabase()

    controlDispatcher := melodyevent.NewEventDispatcher(clockInstance)
    controlDispatcher.AddSubscriber(NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return controlStore
    }))
    controlDispatcher.AddSubscriber(NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database))))

    controlRuntime := deletionRuntime(t, refusing)
    if _, controlErr := controlDispatcher.DispatchName(controlRuntime, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave")); nil != controlErr {
        t.Fatalf("expected the two releases to succeed together, got %v", controlErr)
    }

    if _, found, _ := controlStore.Lookup(controlRuntime, "token-user-4"); true == found || 1 != len(recorder.recordedQueries()) {
        t.Fatalf("expected the control to release the token and the enrollment, found=%v statements %v", found, recorder.recordedQueries())
    }
}
