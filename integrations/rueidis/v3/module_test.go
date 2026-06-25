package rueidis

import (
    "testing"

    "github.com/redis/rueidis"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
)

/* @info fakes */

type fakeClient struct {
    rueidis.Client
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

/* @info tests */

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "rueidis" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "rueidis")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterServicesNoClient(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{AsLocker: true, AsTokenStore: true}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no services without a client, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesClientOnly(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Client: fakeClient{}}).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceClient) {
        t.Fatalf("expected the client service, got %v", registrar.names)
    }
    if true == containsName(registrar.names, melodylock.ServiceLocker) || true == containsName(registrar.names, ServiceTokenStore) {
        t.Fatalf("expected only the client service without the flags, got %v", registrar.names)
    }
}

func TestModule_RegisterServicesAllEnabled(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Client: fakeClient{}, AsLocker: true, AsTokenStore: true}).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceClient) {
        t.Fatalf("expected the client service, got %v", registrar.names)
    }
    if false == containsName(registrar.names, melodylock.ServiceLocker) {
        t.Fatalf("expected the locker service, got %v", registrar.names)
    }
    if false == containsName(registrar.names, ServiceTokenStore) {
        t.Fatalf("expected the token store service, got %v", registrar.names)
    }
}
