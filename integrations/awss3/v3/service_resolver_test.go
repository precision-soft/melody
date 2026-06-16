package awss3

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func TestRegisterStorageServiceUsesCoreStorageName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterStorageService(registrar, nil, "example-bucket")

    if 0 == len(registrar.names) || melodystorage.ServiceStorage != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodystorage.ServiceStorage, registrar.names)
    }
}
