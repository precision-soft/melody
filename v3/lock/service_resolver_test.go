package lock

import (
    "testing"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

func TestLockerServiceName_IsTheRegisteredName(t *testing.T) {
    if "service.lock.locker" != ServiceLocker {
        t.Fatalf("expected the registered service name, got %q", ServiceLocker)
    }
}

func TestLockerMustFromContainerAndResolver_AnswerTheRegisteredService(t *testing.T) {
    serviceContainer := container.NewContainer()

    expected := NewInMemoryLocker(clock.NewSystemClock())

    serviceContainer.MustRegister(
        ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return expected, nil
        },
    )

    if expected != LockerMustFromContainer(serviceContainer) {
        t.Fatalf("expected the registered service from the container")
    }

    if expected != LockerMustFromResolver(serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

func TestLockerMustFromContainerAndResolver_PanicWhenUnregistered(t *testing.T) {
    for _, probe := range []struct {
        name string
        read func()
    }{
        {name: "container", read: func() { _ = LockerMustFromContainer(container.NewContainer()) }},
        {name: "resolver", read: func() { _ = LockerMustFromResolver(container.NewContainer().NewScope()) }},
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
