package outbox

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestOutboxServiceNames_AreTheRegisteredNames(t *testing.T) {
    if "service.outbox.store" != ServiceStore {
        t.Fatalf("expected the registered store name, got %q", ServiceStore)
    }

    if "service.outbox.relay" != ServiceRelay {
        t.Fatalf("expected the registered relay name, got %q", ServiceRelay)
    }
}

func TestRegisterStoreService_RegistersUnderTheNameTheReadersAsk(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    RegisterStoreService(registrar, &Store{})

    if false == containsName(registrar.names, ServiceStore) {
        t.Fatalf("expected the store to be registered under %q, got %v", ServiceStore, registrar.names)
    }
}

func TestRegisterRelayService_RegistersUnderTheNameTheReadersAsk(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    RegisterRelayService(registrar, newModuleTestRelay())

    if false == containsName(registrar.names, ServiceRelay) {
        t.Fatalf("expected the relay to be registered under %q, got %v", ServiceRelay, registrar.names)
    }
}

func TestStoreAndRelayReaders_AnswerWhatTheRegistrarsRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    store := &Store{}
    relay := newModuleTestRelay()

    serviceContainer.MustRegister(ServiceStore, func(resolver containercontract.Resolver) (*Store, error) {
        return store, nil
    })
    serviceContainer.MustRegister(ServiceRelay, func(resolver containercontract.Resolver) (*Relay, error) {
        return relay, nil
    })

    if store != StoreMustFromContainer(serviceContainer) {
        t.Fatal("expected the registered store from the container")
    }

    if relay != RelayMustFromContainer(serviceContainer) {
        t.Fatal("expected the registered relay from the container")
    }

    scope := serviceContainer.NewScope()

    if store != StoreMustFromResolver(scope) {
        t.Fatal("expected the registered store through a scope")
    }

    if relay != RelayMustFromResolver(scope) {
        t.Fatal("expected the registered relay through a scope")
    }
}

func TestStoreAndRelayReaders_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "store container", read: func() { _ = StoreMustFromContainer(container.NewContainer()) }},
        {name: "store resolver", read: func() { _ = StoreMustFromResolver(container.NewContainer().NewScope()) }},
        {name: "relay container", read: func() { _ = RelayMustFromContainer(container.NewContainer()) }},
        {name: "relay resolver", read: func() { _ = RelayMustFromResolver(container.NewContainer().NewScope()) }},
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

var _ ServiceRegistrar = (*spyServiceRegistrar)(nil)
