package session

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func TestSessionServiceResolvers_ResolveTheDeclaredServiceNames(t *testing.T) {
    if "service.session.manager" != ServiceSessionManager {
        t.Fatalf("the session manager service name is a cross-package contract, got %q", ServiceSessionManager)
    }

    if "service.session.storage" != ServiceSessionStorage {
        t.Fatalf("the session storage service name is a cross-package contract, got %q", ServiceSessionStorage)
    }

    serviceContainer := container.NewContainer()

    storage := NewInMemoryStorage()
    defer storage.Close()

    managerInstance := NewManager(storage, time.Hour)

    registerErr := serviceContainer.Register(
        ServiceSessionStorage,
        func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
            return storage, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected storage register error: %v", registerErr)
    }

    registerErr = serviceContainer.Register(
        ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return managerInstance, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected manager register error: %v", registerErr)
    }

    if managerInstance != SessionMustFromContainer(serviceContainer) {
        t.Fatalf("expected the manager door to resolve the declared service")
    }

    if storage != SessionStorageMustFromContainer(serviceContainer) {
        t.Fatalf("expected the storage container door to resolve the declared service")
    }

    if storage != SessionStorageMustFromResolver(serviceContainer) {
        t.Fatalf("expected the storage resolver door to resolve the declared service")
    }
}

func TestSessionServiceResolvers_PanicWhenNothingIsRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing session storage to panic")
        }
    }()

    _ = SessionStorageMustFromResolver(serviceContainer)
}
