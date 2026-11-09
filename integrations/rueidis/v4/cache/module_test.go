package cache

import (
    "testing"
    "time"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type fakeClient struct {
    rueidis.Client
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

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "rueidis.cache" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "rueidis.cache")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service without a client, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Client: fakeClient{}, Backend: BackendConfig{Prefix: "cache"}}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodycache.ServiceCacheBackend != registrar.names[0] {
        t.Fatalf("expected the cache backend service, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesForwardsTheBackendConfig(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    NewModule(ModuleConfig{
        Client:  &teardownSpyClient{},
        Backend: BackendConfig{Prefix: "prefix", CommandTimeout: bound.Of(750 * time.Millisecond)},
    }).RegisterServices(registrar)

    service := container.MustFromResolver[*BackendService](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)

    if 750*time.Millisecond != service.backend.commandTimeout {
        t.Fatalf("expected the module to hand its backend config to the registered backend, got %v", service.backend.commandTimeout)
    }
}

func TestModule_ABackendConfigurationWithNoClientIsRefusedNamingIt(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatal("expected a backend configuration with no client to be refused at boot")
        }

        context := exception.LogContext(recoveredErr, exception.LogContextConfig{})
        if "ModuleConfig.Backend" != context["field"] || "ModuleConfig.Client" != context["requires"] {
            t.Fatalf("expected the refusal to name ModuleConfig.Backend and its requirement, got %v", context)
        }
    }()

    NewModule(ModuleConfig{Backend: BackendConfig{Prefix: "cache"}}).RegisterServices(&spyServiceRegistrar{})
}

type typedNilModuleClient struct {
    rueidis.Client
}

func TestModule_ATypedNilClientIsNoClient(t *testing.T) {
    var client *typedNilModuleClient

    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Client: client}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no service over a typed nil client, got %v", registrar.names)
    }

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "ModuleConfig.Backend" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected a backend configuration over a typed nil client to be refused naming ModuleConfig.Backend, got %v", recoveredErr)
        }
    }()

    NewModule(ModuleConfig{Client: client, Backend: BackendConfig{Prefix: "cache"}}).RegisterServices(&spyServiceRegistrar{})
}
