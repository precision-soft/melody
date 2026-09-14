package storage

import (
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func TestStorageServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.storage.storage" != ServiceStorage {
        t.Fatalf("expected the registered service name, got %q", ServiceStorage)
    }
}

func TestStorageMustFromContainerAndResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer()

    expected := NewLocalStorage(t.TempDir())

    serviceContainer.MustRegister(
        ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            return expected, nil
        },
    )

    if expected != StorageMustFromContainer(serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != StorageMustFromResolver(serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestStorageMustFromContainerAndResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = StorageMustFromContainer(container.NewContainer()) }},
        {name: "resolver", read: func() { _ = StorageMustFromResolver(container.NewContainer().NewScope()) }},
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("%s: expected the strict reader to panic when nothing is registered", probe.name)
                }
            }()

            probe.read()
        }()
    }
}
