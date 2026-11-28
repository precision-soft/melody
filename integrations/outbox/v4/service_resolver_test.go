package outbox

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestOutboxServiceNames_AreTheRegisteredNames(t *testing.T) {
    if "service.outbox.store" != ServiceStore {
        t.Fatalf("expected the registered store name, got %q", ServiceStore)
    }

    if "service.outbox.relay" != ServiceRelay {
        t.Fatalf("expected the registered relay name, got %q", ServiceRelay)
    }
}

func TestStoreAndRelayReaders_AnswerWhatTheRegistrarsRegistered(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    store := &Store{}
    relay := newModuleTestRelay()

    serviceContainer.MustRegister(ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        return store, nil
    }, container.RegisterOptions{})
    serviceContainer.MustRegister(ServiceRelay, func(resolver containercontract.Resolver) (*Relay, error) {
        return relay, nil
    }, container.RegisterOptions{})

    if store != StoreMustFromResolver(t.Context(), serviceContainer) {
        t.Fatal("expected the registered store from the container")
    }

    if relay != RelayMustFromResolver(t.Context(), serviceContainer) {
        t.Fatal("expected the registered relay from the container")
    }

    scope := serviceContainer.NewScope()

    if store != StoreMustFromResolver(t.Context(), scope) {
        t.Fatal("expected the registered store through a scope")
    }

    if relay != RelayMustFromResolver(t.Context(), scope) {
        t.Fatal("expected the registered relay through a scope")
    }
}

func TestStoreAndRelayReaders_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "store container", read: func() { _ = StoreMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "store resolver", read: func() { _ = StoreMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
        {name: "relay container", read: func() { _ = RelayMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "relay resolver", read: func() { _ = RelayMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("%s: expected the strict reader to panic when nothing is registered", probe.name)
                }
            }()

            probe.read()
        }()
    }
}
