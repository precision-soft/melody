package lock

import (
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func TestLockerServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.lock.locker" != ServiceLocker {
        t.Fatalf("expected the registered service name, got %q", ServiceLocker)
    }
}

func TestLockerMustFromResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    expected := NewInMemoryLocker(InMemoryLockerConfig{})

    serviceContainer.MustRegister(
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return expected, nil
        },
        container.RegisterOptions{},
    )

    if expected != LockerMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != LockerMustFromResolver(t.Context(), serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestLockerMustFromResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = LockerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) }},
        {name: "resolver", read: func() { _ = LockerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) }},
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
