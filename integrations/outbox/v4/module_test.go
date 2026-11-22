package outbox

import (
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
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

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    instance.RegisterService(serviceName, provider, options)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.RegisterService(serviceName, provider, options)
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
    return NewRelay(&fakeRepository{}, &fakeTransport{}, &stringCodec{}, RelayConfig{})
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

func newModuleTestRelayFactory() func(resolver containercontract.Resolver) (*Relay, error) {
    return func(resolver containercontract.Resolver) (*Relay, error) { return newModuleTestRelay(), nil }
}

func TestModule_RegisterServicesSkipsNilServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    NewModule(ModuleConfig{}).RegisterServices(registrar)

    if 0 != len(registrar.names) {
        t.Fatalf("expected no services registered from an empty config, got %v", registrar.names)
    }
}

func TestModule_RegisterCliCommandsExposesRelayCommand(t *testing.T) {
    module := NewModule(ModuleConfig{Relay: newModuleTestRelayFactory()})

    commands := module.RegisterCliCommands(nil)
    if 1 != len(commands) {
        t.Fatalf("expected exactly one command, got %d", len(commands))
    }

    if "melody:outbox:relay" != commands[0].Name() {
        t.Fatalf("unexpected command name %q", commands[0].Name())
    }
}

func TestModule_RegisterCliCommandsHandsTheCommandItsConfig(t *testing.T) {
    config := RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(3 * time.Second)}}

    commands := NewModule(ModuleConfig{Relay: newModuleTestRelayFactory(), RelayCommand: config}).RegisterCliCommands(nil)
    if 1 != len(commands) || 3*time.Second != commands[0].(*RelayCommand).errorBackoff.Max {
        t.Fatalf("expected the relay command to carry the given 3s cap, got %v", commands)
    }
}

func TestModule_RegisterCliCommandsRefusesAnErrorBackoffOutsideItsRule(t *testing.T) {
    expectPanicNamingField(t, "RelayCommandConfig.ErrorBackoff.Max", func() {
        NewModule(ModuleConfig{Relay: newModuleTestRelayFactory(), RelayCommand: RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(time.Microsecond)}}}).RegisterCliCommands(nil)
    })
}

func TestModule_RegisterCliCommandsSkipsWithoutRelay(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if commands := module.RegisterCliCommands(nil); 0 != len(commands) {
        t.Fatalf("expected no commands without a relay, got %d", len(commands))
    }
}

type containerServiceRegistrar struct {
    containercontract.Container
}

func (instance *containerServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.MustRegister(serviceName, provider, options)
}

func TestModule_RegisterServicesRegistersTheFactoriesAsProvidersRunAtFirstResolution(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    store := &Store{}
    relay := newModuleTestRelay()
    storeCalls := 0
    relayCalls := 0

    NewModule(ModuleConfig{
        Store: func(resolver containercontract.Resolver) (*Store, error) {
            storeCalls++

            return store, nil
        },
        Relay: func(resolver containercontract.Resolver) (*Relay, error) {
            relayCalls++

            return relay, nil
        },
    }).RegisterServices(&containerServiceRegistrar{Container: serviceContainer})

    if 0 != storeCalls || 0 != relayCalls {
        t.Fatalf("expected no factory to run at registration, ran store %d relay %d", storeCalls, relayCalls)
    }

    if store != StoreMustFromResolver(t.Context(), serviceContainer) || relay != RelayMustFromResolver(t.Context(), serviceContainer) {
        t.Fatal("expected the factories' instances under service.outbox.store and service.outbox.relay")
    }

    if 1 != storeCalls || 1 != relayCalls {
        t.Fatalf("expected each factory to run once at first resolution, ran store %d relay %d", storeCalls, relayCalls)
    }
}

func TestModule_ARelayCommandConfigurationWithNoRelayIsRefusedNamingIt(t *testing.T) {
    expectPanicNamingField(t, "ModuleConfig.RelayCommand", func() {
        NewModule(ModuleConfig{RelayCommand: RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(3 * time.Second)}}}).RegisterServices(&spyServiceRegistrar{})
    })

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "ModuleConfig.Relay" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["requires"] {
            t.Fatalf("expected the refusal to require ModuleConfig.Relay, got %v", recoveredErr)
        }
    }()

    NewModule(ModuleConfig{RelayCommand: RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(3 * time.Second)}}}).RegisterServices(&spyServiceRegistrar{})
}

func TestModule_ARelayCommandConfigurationBesideARelayIsAdmitted(t *testing.T) {
    NewModule(ModuleConfig{Relay: newModuleTestRelayFactory(), RelayCommand: RelayCommandConfig{ErrorBackoff: bound.Backoff{Max: bound.Of(3 * time.Second)}}}).RegisterServices(&spyServiceRegistrar{})
}

func (instance *spyServiceRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}
