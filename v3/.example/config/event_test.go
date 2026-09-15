package config

import (
    "context"
    "errors"
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodycache "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
)

func TestRequestBudgetConfig_ChargesTheForwardedClientRatherThanTheProxy(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7"); "203.0.113.7" != key {
        t.Fatalf("expected the forwarded client to be charged, got %q", key)
    }
}

func TestRequestBudgetConfig_IgnoresAForwardedHeaderFromAnUntrustedPeer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "198.51.100.4", "203.0.113.7"); "198.51.100.4" != key {
        t.Fatalf("expected a header from an untrusted peer to be ignored, got %q", key)
    }
}

func TestRequestBudgetConfig_ReadsTheDockerGatewayAsTheClientRatherThanAsAHop(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected the gateway the balancer forwarded to be the client, got %q", key)
    }

    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7, 172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected a header the client sent through the balancer to stop at the gateway, got %q", key)
    }
}

func TestRequestBudgetConfig_DoesNotBelieveANeighbouringContainer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "172.18.0.11", "203.0.113.7"); "172.18.0.11" != key {
        t.Fatalf("expected a header from a neighbouring container to be ignored, got %q", key)
    }
}

func TestRequestBudgetConfig_AnEmptyListChargesThePeer(t *testing.T) {
    if key := resolvedBudgetKey(t, nil, balancerAddress, "203.0.113.7"); balancerAddress != key {
        t.Fatalf("expected an empty trusted list to charge the peer, got %q", key)
    }
}

func TestRegisterSubscribers_InstallsTheTwoFactorEnrollmentReleaseWhenThereIsAStore(t *testing.T) {
    ownersOnDeletion := func(moduleInstance *Module) []string {
        eventDispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
        moduleInstance.registerSubscribers(eventDispatcher)

        var owners []string
        for _, registered := range eventDispatcher.RegisteredEvents() {
            if event.UserDeletedEventName != registered.EventName {
                continue
            }

            for _, listener := range registered.Listeners {
                owners = append(owners, listener.Owner)
            }
        }

        return owners
    }

    without := ownersOnDeletion(moduleWithEnvironment(t, map[string]string{}))

    withStore := moduleWithEnvironment(t, map[string]string{})
    withStore.twoFactorStore = twofactor.NewStore(newUndialedDatabase())
    with := ownersOnDeletion(withStore)

    if len(without) != len(with) {
        t.Fatalf("expected cleanup to keep one combined owner on %s, got %v without and %v with", event.UserDeletedEventName, without, with)
    }

    found := false
    for _, owner := range with {
        if true == strings.Contains(owner, "UserEventSubscriber") {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the combined user cleanup subscriber among the owners of %s, got %v", event.UserDeletedEventName, with)
    }
}

func TestRegisterSubscribers_UserDeletionAttemptsStoreAfterCacheFailure(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    database := newUndialedDatabase()
    defer database.Close()
    moduleInstance.twoFactorStore = twofactor.NewStore(database)
    dispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
    moduleInstance.registerSubscribers(dispatcher)
    cacheFailure := errors.New("cache failure")
    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(melodycache.ServiceCache, func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
        return &deletionFailureCache{failure: cacheFailure}, nil
    })
    containerInstance.MustRegister(melodylogging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))
    if false == errors.Is(dispatchErr, cacheFailure) || false == errors.Is(dispatchErr, errUndialedDatabase) {
        t.Fatalf("expected both cleanup failures through configured dispatcher: %v", dispatchErr)
    }
}
