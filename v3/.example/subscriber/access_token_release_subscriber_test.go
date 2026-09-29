package subscriber

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/.example/event"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestAccessTokenReleaseSubscriber_DeletesTheDeviceTokensOfTheDeletedAccountOnly(t *testing.T) {
    store := melodysecurity.NewInMemoryTokenStore()
    store.Put("token-deleted-phone", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "phone"})
    store.Put("token-deleted-laptop", melodysecuritycontract.Claims{UserIdentifier: "user-4", DeviceIdentifier: "laptop"})
    store.Put("token-kept", melodysecuritycontract.Claims{UserIdentifier: "user-5", DeviceIdentifier: "phone"})

    subscriberInstance := NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.RevocableTokenStore {
        return store
    })

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listeners := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.UserDeletedEventName, len(listeners))
    }

    deleted := melodyevent.NewEvent(event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "deleted"), melodyclock.NewSystemClock())
    if listenErr := listeners[0].Listener()(runtimeInstance, deleted); nil != listenErr {
        t.Fatalf("expected the release to answer nil, got %v", listenErr)
    }

    for tokenString, expected := range map[string]bool{"token-deleted-phone": false, "token-deleted-laptop": false, "token-kept": true} {
        _, found, lookupErr := store.Lookup(runtimeInstance, tokenString)
        if nil != lookupErr || expected != found {
            t.Fatalf("expected %s found=%t after the deletion, got found=%t (%v)", tokenString, expected, found, lookupErr)
        }
    }
}
