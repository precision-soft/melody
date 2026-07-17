package outbox

import (
    "fmt"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func assertPanicsWithMessage(t *testing.T, expected string, callback func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a panic containing %q", expected)
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), expected) {
            t.Fatalf("expected the panic to mention %q, got %v", expected, recovered)
        }
    }()

    callback()
}

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func containsName(names []string, want string) bool {
    for _, name := range names {
        if want == name {
            return true
        }
    }

    return false
}

func newModuleTestRelay() *Relay {
    return NewRelay(RelayConfig{
        Repository: &fakeRepository{},
        Transport:  &fakeTransport{},
        Codec:      &stringCodec{},
    })
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "outbox" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "outbox")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServicesUsesCanonicalNames(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    NewModule(ModuleConfig{
        Store: &Store{},
        Relay: newModuleTestRelay(),
    }).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceStore) {
        t.Fatalf("expected the store registered under %q, got %v", ServiceStore, registrar.names)
    }

    if false == containsName(registrar.names, ServiceRelay) {
        t.Fatalf("expected the relay registered under %q, got %v", ServiceRelay, registrar.names)
    }
}

func TestModule_RegisterServicesSkipsNilServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    NewModule(ModuleConfig{}).RegisterServices(registrar)

    if 0 != len(registrar.names) {
        t.Fatalf("expected no services registered from an empty config, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesPanicsOnStoreAndFactoryBothSet(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    assertPanicsWithMessage(t, "outbox module received both a store and a store factory - set exactly one", func() {
        NewModule(ModuleConfig{
            Store: &Store{},
            StoreFactory: func(resolver containercontract.Resolver) (*Store, error) {
                return &Store{}, nil
            },
        }).RegisterServices(registrar)
    })

    if 0 != len(registrar.names) {
        t.Fatalf("expected no service registered before the panic, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesPanicsOnRelayAndFactoryBothSet(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    assertPanicsWithMessage(t, "outbox module received both a relay and a relay factory - set exactly one", func() {
        NewModule(ModuleConfig{
            Relay: newModuleTestRelay(),
            RelayFactory: func(resolver containercontract.Resolver) (*Relay, error) {
                return newModuleTestRelay(), nil
            },
        }).RegisterServices(registrar)
    })

    if 0 != len(registrar.names) {
        t.Fatalf("expected no service registered before the panic, got %v", registrar.names)
    }
}

func TestModule_RegisterCliCommandsExposesRelayCommand(t *testing.T) {
    module := NewModule(ModuleConfig{Relay: newModuleTestRelay()})

    commands := module.RegisterCliCommands(nil)
    if 1 != len(commands) {
        t.Fatalf("expected exactly one command, got %d", len(commands))
    }

    if "melody:outbox:relay" != commands[0].Name() {
        t.Fatalf("unexpected command name %q", commands[0].Name())
    }
}

func TestModule_RegisterCliCommandsSkipsWithoutRelay(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if commands := module.RegisterCliCommands(nil); 0 != len(commands) {
        t.Fatalf("expected no commands without a relay, got %d", len(commands))
    }
}
