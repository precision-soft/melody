package outbox

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestModule_RegisterServicesRegistersFactoryProviders(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    NewModule(ModuleConfig{
        StoreFactory: func(resolver containercontract.Resolver) (*Store, error) {
            return &Store{}, nil
        },
        RelayFactory: func(resolver containercontract.Resolver) (*Relay, error) {
            return newModuleTestRelay(), nil
        },
    }).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceStore) {
        t.Fatalf("expected the store factory registered under %q, got %v", ServiceStore, registrar.names)
    }

    if false == containsName(registrar.names, ServiceRelay) {
        t.Fatalf("expected the relay factory registered under %q, got %v", ServiceRelay, registrar.names)
    }
}

func TestNewRelayCommandFromResolver_DefersResolution(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolved := false
    container.MustRegister[*Relay](
        serviceContainer,
        ServiceRelay,
        func(resolver containercontract.Resolver) (*Relay, error) {
            resolved = true

            return newModuleTestRelay(), nil
        },
    )

    command := NewRelayCommandFromResolver(serviceContainer)

    if true == resolved {
        t.Fatalf("expected no relay resolution before the first batch")
    }

    if "melody:outbox:relay" != command.Name() {
        t.Fatalf("unexpected command name %q", command.Name())
    }
}
