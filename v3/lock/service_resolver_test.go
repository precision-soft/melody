package lock

import (
    "testing"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

/* the service NAME is the whole wiring: a name that drifts from the one the module registers under leaves every reader blind while everything still compiles, so the constant is asserted by value rather than through itself. */
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

    /* the resolver-taking door is what a scoped service reads through: a scope is a Resolver and not a Container, so the two doors are not interchangeable at the call site */
    if expected != LockerMustFromResolver(serviceContainer.NewScope()) {
        t.Fatalf("expected the registered service through a scope")
    }
}

/* the strict readers are the boot-time ones: they panic rather than hand back a nil the caller would dereference later, at a point where nothing names the missing registration */
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
