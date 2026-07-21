package awss3

import (
    "testing"

    "github.com/minio/minio-go/v7"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
)

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

    if "awss3" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "awss3")
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
    NewModule(ModuleConfig{Client: &minio.Client{}, Bucket: "bucket"}).RegisterServices(registrar)
    if 1 != len(registrar.names) || melodystorage.ServiceStorage != registrar.names[0] {
        t.Fatalf("expected the storage service, got %v", registrar.names)
    }
}
