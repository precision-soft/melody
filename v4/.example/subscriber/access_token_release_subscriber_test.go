package subscriber

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestAccessTokenReleaseSubscriber_DeletesTheDeviceTokensOfTheDeletedAccountOnly(t *testing.T) {
    store := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    store.Put(releaseTestRuntime(), "token-deleted-phone", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())
    store.Put(releaseTestRuntime(), "token-deleted-laptop", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())
    store.Put(releaseTestRuntime(), "token-kept", melodysecuritycontract.Claims{UserIdentifier: "user-5", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())

    subscriberInstance := NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return store
    })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listeners := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.UserDeletedEventName, len(listeners))
    }

    deleted := melodyevent.NewEvent(event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "deleted"), melodyclock.NewSystemClock().Now())
    if listenErr := listeners[0].Listener()(runtimeInstance, deleted); nil != listenErr {
        t.Fatalf("expected the release to answer nil, got %v", listenErr)
    }

    for tokenString, expected := range map[string]bool{"token-deleted-phone": false, "token-deleted-laptop": false, "token-kept": true} {
        _, found, lookupErr := store.Lookup(runtimeInstance, tokenString)
        if nil != lookupErr || expected != found {
            t.Fatalf("expected %s found=%t after the deletion, got found=%t (%v)", tokenString, expected, found, lookupErr)
        }
    }

    /* the account-wide epoch at the deletion's instant refuses the tokens no store holds — the bearer tokens — and is written for the deleted account alone */
    deletedEpoch, deletedErr := store.RevocationEpoch(runtimeInstance, "user-4", "")
    if nil != deletedErr || false == deletedEpoch.Equal(deleted.Timestamp()) {
        t.Fatalf("expected the deleted account revoked up to the deletion's instant %v, got %v (%v)", deleted.Timestamp(), deletedEpoch, deletedErr)
    }

    if keptEpoch, _ := store.RevocationEpoch(runtimeInstance, "user-5", ""); false == keptEpoch.IsZero() {
        t.Fatalf("expected no epoch for the account that was not deleted, got %v", keptEpoch)
    }
}

func releaseTestRuntime() melodyruntimecontract.Runtime {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

type revocationFailingTokenStore struct {
    *melodysecurity.InMemoryTokenStore
    revokeErr error
    deleteErr error
}

func (instance *revocationFailingTokenStore) RevokeBefore(_ melodyruntimecontract.Runtime, _ string, _ string, _ time.Time) error {
    return instance.revokeErr
}

func (instance *revocationFailingTokenStore) DeleteByUser(_ melodyruntimecontract.Runtime, _ string) (int, error) {
    return 0, instance.deleteErr
}

func TestAccessTokenReleaseSubscriber_AFailedRevocationIsReturned(t *testing.T) {
    revokeErr := errors.New("the revocation could not be published")
    deleteErr := errors.New("the tokens could not be released")
    store := &revocationFailingTokenStore{InMemoryTokenStore: melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}), revokeErr: revokeErr, deleteErr: deleteErr}

    subscriberInstance := NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return store
    })

    deleted := melodyevent.NewEvent(event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "deleted"), melodyclock.NewSystemClock().Now())
    listenErr := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName][0].Listener()(releaseTestRuntime(), deleted)

    if false == errors.Is(listenErr, revokeErr) || false == errors.Is(listenErr, deleteErr) {
        t.Fatalf("expected both failures returned, got %v", listenErr)
    }
}

func TestAccessTokenReleaseSubscriber_ACancelledRequestStillPublishesTheBoundary(t *testing.T) {
    store := melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})
    store.Put(releaseTestRuntime(), "token-held", melodysecuritycontract.Claims{UserIdentifier: "user-6", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())

    subscriberInstance := NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
        return store
    })

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    cancelledRuntime := melodyruntime.New(cancelledContext, containerInstance.NewScope(), containerInstance)

    deleted := melodyevent.NewEvent(event.UserDeletedEventName, event.NewUserDeletedEvent("user-6", "deleted"), melodyclock.NewSystemClock().Now())
    if listenErr := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName][0].Listener()(cancelledRuntime, deleted); nil != listenErr {
        t.Fatalf("expected the release to outlive the request's cancellation, got %v", listenErr)
    }

    if epoch, _ := store.RevocationEpoch(releaseTestRuntime(), "user-6", ""); false == epoch.Equal(deleted.Timestamp()) {
        t.Fatalf("expected the boundary published at the deletion's instant, got %v", epoch)
    }

    if _, found, _ := store.Lookup(releaseTestRuntime(), "token-held"); true == found {
        t.Fatalf("expected the held token released")
    }
}
