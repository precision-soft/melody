package cache

import (
    "testing"
    "time"

    "github.com/redis/rueidis"

    melodycache "github.com/precision-soft/melody/v3/cache"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type fakeClient struct {
    rueidis.Client
}

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    instance.RegisterService(serviceName, provider, options...)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.RegisterService(serviceName, provider, options...)
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
    NewModule(ModuleConfig{Client: fakeClient{}, Prefix: "cache"}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodycache.ServiceCacheBackend != registrar.names[0] {
        t.Fatalf("expected the cache backend service, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesForwardsTheBackendOptions(t *testing.T) {
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    NewModule(ModuleConfig{
        Client:         &teardownSpyClient{},
        Prefix:         "prefix",
        BackendOptions: []BackendOption{WithCommandTimeout(750 * time.Millisecond)},
    }).RegisterServices(registrar)

    service := container.MustFromResolver[*BackendService](serviceContainer, melodycache.ServiceCacheBackend)

    if 750*time.Millisecond != service.backend.commandTimeout {
        t.Fatalf("expected the module to hand its backend options to the registered backend, got %v", service.backend.commandTimeout)
    }
}
