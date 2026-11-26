package cache

import (
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

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
    NewModule(ModuleConfig{Backend: BackendConfig{Prefix: "cache"}}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodycache.ServiceCacheBackend != registrar.names[0] {
        t.Fatalf("expected the cache backend service, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesForwardsTheBackendConfig(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionOver(serviceContainer, &teardownSpyClient{})
    NewModule(ModuleConfig{
        Backend: BackendConfig{Prefix: "prefix", CallTimeout: bound.Of(750 * time.Millisecond)},
    }).RegisterServices(registrar)

    service := container.MustFromResolver[*BackendService](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)

    if 750*time.Millisecond != service.backend.callTimeout {
        t.Fatalf("expected the module to hand its backend config to the registered backend, got %v", service.backend.callTimeout)
    }
}

func TestModule_TheBackendDeclaresTheConnectionItIsClosedBefore(t *testing.T) {
    var options containercontract.RegisterOptions
    registrar := &optionsRecordingRegistrar{options: &options}

    NewModule(ModuleConfig{}).RegisterServices(registrar)

    if 1 != len(options.TeardownDependencyNames) || melodyrueidis.ServiceConnection != options.TeardownDependencyNames[0] {
        t.Fatalf("expected the backend to declare the connection, got %v", options.TeardownDependencyNames)
    }
}

type optionsRecordingRegistrar struct {
    spyServiceRegistrar
    options *containercontract.RegisterOptions
}

func (instance *optionsRecordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    *instance.options = options
}

func (instance *spyServiceRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}
