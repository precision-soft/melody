package subscriber

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* deletionRuntime is a runtime over a container holding the cache the user listener drops entries from and a silent journal */
func deletionRuntime(t *testing.T, cacheInstance melodycachecontract.Cache) melodycontainercontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(containerInstance, melodycache.ServiceCache, func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
        return cacheInstance, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    }, melodycontainer.RegisterOptions{})

    return melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
}

/* the three listeners of the deletion, registered in the composition root's order: the cache's first, the token release, then the two-factor release over the given store source, the event declared as the composition root declares it */
func deletionDispatcher(tokenStore melodysecuritycontract.EpochRevocableTokenStore, twoFactorStore twofactor.StoreSource) *melodyevent.EventDispatcher {
    dispatcher := undeclaredDeletionDispatcher(tokenStore, twoFactorStore)
    dispatcher.DeclareEvent(event.EventUserDeleted, event.UserDeletedEventConfig())

    return dispatcher
}

/* undeclaredDeletionDispatcher is the deletion's three listeners with no declaration, the control of the tag pins */
func undeclaredDeletionDispatcher(tokenStore melodysecuritycontract.EpochRevocableTokenStore, twoFactorStore twofactor.StoreSource) *melodyevent.EventDispatcher {
    dispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    dispatcher.AddSubscriber(NewUserEventSubscriber())
    dispatcher.AddSubscriber(NewAccessTokenReleaseSubscriber(func(runtimeInstance melodycontainercontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return tokenStore
    }))
    dispatcher.AddSubscriber(NewTwoFactorEnrollmentSubscriber(twoFactorStore))

    return dispatcher
}

/* the token release outranks every other listener of the deletion, read off the real subscribers: the order the listeners run in when nothing fails, the tokens first */
func TestUserDeleted_TheTokenReleaseOutranksTheOtherListeners(t *testing.T) {
    releasePriority := NewAccessTokenReleaseSubscriber(nil).SubscribedEvents()[event.EventUserDeleted][0].Config().Priority
    twoFactorPriority := NewTwoFactorEnrollmentSubscriber(nil).SubscribedEvents()[event.EventUserDeleted][0].Config().Priority
    cachePriority := NewUserEventSubscriber().SubscribedEvents()[event.EventUserDeleted][0].Config().Priority

    if releasePriority <= twoFactorPriority || twoFactorPriority <= cachePriority {
        t.Fatalf("expected the token release above the two-factor release above the cache listener, got %d, %d, %d", releasePriority, twoFactorPriority, cachePriority)
    }
}

/* the two-factor release panicking on its store, as a redis-backed store answers an outage, fails the dispatch, and the cache behind it, tagged, runs and refuses its deletes: the deleted account's device token is still gone and its account revoked. The tag, not the order, is what protects the release: see the untagged failing listener ahead of it below. The control, the two releases over healthy stores, answers nil with the token and the enrollment released. */
func TestUserDeleted_AFailingListenerDoesNotLeaveTheDeletedAccountsTokens(t *testing.T) {
    clockInstance := melodyclock.NewSystemClock()
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance})
    refusing := &refusingCache{Cache: melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})}

    tokenStore := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    tokenStore.Put(releaseTestRuntime(), "token-user-4", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"}, melodybound.UnlimitedDuration())

    panicking := func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        panic(errors.New("the two-factor store is unreachable"))
    }

    runtimeInstance := deletionRuntime(t, refusing)
    _, dispatchErr := deletionDispatcher(tokenStore, panicking).DispatchName(runtimeInstance, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))
    if nil == dispatchErr {
        t.Fatal("expected the dispatch to fail on the panicking two-factor release")
    }

    if _, found, _ := tokenStore.Lookup(runtimeInstance, "token-user-4"); true == found {
        t.Fatal("expected the deleted account's device token released ahead of the listeners that failed")
    }

    if epoch, _ := tokenStore.RevocationEpoch(runtimeInstance, "user-4", melodysecuritycontract.RevocationOptions{}); true == epoch.IsZero() {
        t.Fatal("expected the deleted account revoked ahead of the listeners that failed")
    }

    controlStore := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    controlStore.Put(releaseTestRuntime(), "token-user-4", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"}, melodybound.UnlimitedDuration())
    database, recorder := newRecordingDatabase()

    controlDispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: clockInstance})
    controlDispatcher.AddSubscriber(NewAccessTokenReleaseSubscriber(func(runtimeInstance melodycontainercontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return controlStore
    }))
    controlDispatcher.AddSubscriber(NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database))))

    controlRuntime := deletionRuntime(t, refusing)
    if _, controlErr := controlDispatcher.DispatchName(controlRuntime, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave")); nil != controlErr {
        t.Fatalf("expected the two releases to succeed together, got %v", controlErr)
    }

    if _, found, _ := controlStore.Lookup(controlRuntime, "token-user-4"); true == found || 2 != len(recorder.recordedQueries()) {
        t.Fatalf("expected the control to release the token and the enrollment, found=%v statements %v", found, recorder.recordedQueries())
    }
}

/* recordingDeleteCache records the keys the cache listener drops */
type recordingDeleteCache struct {
    melodycachecontract.Cache
    deletedKeys []string
}

func (instance *recordingDeleteCache) Delete(runtimeInstance melodycontainercontract.Runtime, key string) error {
    instance.deletedKeys = append(instance.deletedKeys, key)

    return nil
}

func newRecordingDeleteCache() *recordingDeleteCache {
    clockInstance := melodyclock.NewSystemClock()
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance})

    return &recordingDeleteCache{Cache: melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})}
}

func twoFactorReleaseCount(recorder *recordingDriver) int {
    released := 0
    for _, statement := range recorder.recordedQueries() {
        if true == strings.HasPrefix(statement, "DELETE FROM `melody_example_v4_two_factor`") && true == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
            released++
        }
    }

    return released
}

/* an outage of the token store fails the release at the head of the deletion; the event declares the release and cache tags, so the two-factor release and the cache drop behind it still run, and the answer carries the outage */
func TestUserDeleted_ATokenStoreOutageStillReleasesTheSecondFactorAndDropsTheCache(t *testing.T) {
    outage := errors.New("the token store is unreachable")
    tokenStore := &revocationFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), deleteErr: outage}
    database, recorder := newRecordingDatabase()
    cacheInstance := newRecordingDeleteCache()

    runtimeInstance := deletionRuntime(t, cacheInstance)
    _, dispatchErr := deletionDispatcher(tokenStore, fixedTwoFactorStore(twofactor.NewStore(database))).DispatchName(runtimeInstance, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))

    if false == errors.Is(dispatchErr, outage) {
        t.Fatalf("expected the answer to carry the outage, got %v", dispatchErr)
    }

    if 1 != twoFactorReleaseCount(recorder) {
        t.Fatalf("expected the enrollment released behind the failed token release, got %v", recorder.recordedQueries())
    }

    if 3 != len(cacheInstance.deletedKeys) {
        t.Fatalf("expected the three cache keys dropped behind the failed token release, got %v", cacheInstance.deletedKeys)
    }
}

/* the release is protected by its tag, not its rank: an untagged listener an application adds above it and that fails does not skip it */
func TestUserDeleted_AFailingUntaggedListenerAheadOfTheReleaseDoesNotSkipIt(t *testing.T) {
    tokenStore := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    tokenStore.Put(releaseTestRuntime(), "token-user-4", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"}, melodybound.UnlimitedDuration())
    database, _ := newRecordingDatabase()

    applicationFailure := errors.New("the application's listener failed")
    dispatcher := deletionDispatcher(tokenStore, fixedTwoFactorStore(twofactor.NewStore(database)))
    dispatcher.AddListener(
        event.EventUserDeleted,
        func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
            return applicationFailure
        },
        melodyeventcontract.ListenerConfig{Priority: 30},
    )

    runtimeInstance := deletionRuntime(t, newRecordingDeleteCache())
    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))

    if false == errors.Is(dispatchErr, applicationFailure) {
        t.Fatalf("expected the application's failure answered, got %v", dispatchErr)
    }

    if _, found, _ := tokenStore.Lookup(runtimeInstance, "token-user-4"); true == found {
        t.Fatal("expected the token released behind the failing untagged listener")
    }
}

/* the control: without the declaration the dispatch ends at the failed token release, and neither the two-factor release nor the cache drop runs, so the declaration is what runs them */
func TestUserDeleted_WithoutTheDeclarationTheReleasesBehindAFailureDoNotRun(t *testing.T) {
    tokenStore := &revocationFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), deleteErr: errors.New("the token store is unreachable")}
    database, recorder := newRecordingDatabase()
    cacheInstance := newRecordingDeleteCache()

    runtimeInstance := deletionRuntime(t, cacheInstance)
    _, _ = undeclaredDeletionDispatcher(tokenStore, fixedTwoFactorStore(twofactor.NewStore(database))).DispatchName(runtimeInstance, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))

    if 0 != twoFactorReleaseCount(recorder) || 0 != len(cacheInstance.deletedKeys) {
        t.Fatalf("expected nothing behind the failed release to run without the declaration, got %v and %v", recorder.recordedQueries(), cacheInstance.deletedKeys)
    }
}
