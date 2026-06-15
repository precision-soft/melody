package cache

import (
    "testing"

    "github.com/redis/rueidis"

    melodycache "github.com/precision-soft/melody/v3/cache"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/** @info fakes */

type fakeClient struct {
    rueidis.Client
}

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

/** @info tests */

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
