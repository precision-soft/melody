package pgsql

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

func TestRegisterLockerServiceUsesTheFrameworkLockerName(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    RegisterLockerService(registrar, newUndialedDatabase())

    if 1 != len(registrar.names) {
        t.Fatalf("expected exactly one registration, got %v", registrar.names)
    }

    if melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %q", melodylock.ServiceLocker, registrar.names[0])
    }
}

/* the name is half the contract; the other half is that the provider actually answers a Locker built over the handle it was given, which is what an application resolving the framework's locker name receives. */
func TestRegisterLockerServiceProviderAnswersAPgsqlLocker(t *testing.T) {
    var captured any

    registrar := &capturingServiceRegistrar{}
    RegisterLockerService(registrar, newUndialedDatabase())
    captured = registrar.provider

    provider, ok := captured.(func(containercontract.Resolver) (lockcontract.Locker, error))
    if false == ok {
        t.Fatalf("expected a locker provider, got %T", captured)
    }

    locker, lockerErr := provider(nil)
    if nil != lockerErr {
        t.Fatalf("provider returned an error: %v", lockerErr)
    }

    if _, isPgsql := locker.(*Locker); false == isPgsql {
        t.Fatalf("expected a pgsql Locker, got %T", locker)
    }
}

type capturingServiceRegistrar struct {
    provider any
}

func (instance *capturingServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.provider = provider
}
