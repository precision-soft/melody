package storage

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

func TestStorageServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.storage.storage" != ServiceStorage {
        t.Fatalf("expected the registered service name, got %q", ServiceStorage)
    }
}

func TestStorageMustFromResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    expected := NewLocalStorage(t.TempDir(), LocalStorageConfig{})

    serviceContainer.MustRegister(
        ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            return expected, nil
        },
        container.RegisterOptions{},
    )

    if expected != StorageMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != StorageMustFromResolver(t.Context(), serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestStorageMustFromResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = StorageMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "resolver", read: func() { _ = StorageMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
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
