package pgsql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

func TestRegisterLockerServiceUsesTheFrameworkLockerName(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    RegisterLockerService(registrar, newUndialedDatabase(), LockerConfig{})

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
    RegisterLockerService(registrar, newUndialedDatabase(), LockerConfig{})
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
    names    []string
    provider any
}

func (instance *capturingServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
    instance.provider = provider
}

func TestRegisterLockerService_TheRegisteredLockerCarriesTheConfiguredReleaseTimeout(t *testing.T) {
    registrar := &capturingServiceRegistrar{}

    RegisterLockerService(registrar, newUndialedDatabase(), LockerConfig{ReleaseTimeout: bound.Of(2 * time.Second)})

    provider, isProvider := registrar.provider.(func(containercontract.Resolver) (lockcontract.Locker, error))
    if false == isProvider {
        t.Fatalf("expected a locker provider, got %T", registrar.provider)
    }

    locker, lockerErr := provider(nil)
    if nil != lockerErr || 2*time.Second != locker.(*Locker).releaseTimeout {
        t.Fatalf("expected the configured release timeout on the registered locker, got %v %v", locker, lockerErr)
    }
}

func TestRegisterLockerService_RefusesAReleaseTimeoutOutsideItsRuleAtTheCall(t *testing.T) {
    registrar := &capturingServiceRegistrar{}

    expectPanicNamingField(t, "LockerConfig.ReleaseTimeout", func() {
        RegisterLockerService(registrar, newUndialedDatabase(), LockerConfig{ReleaseTimeout: bound.UnlimitedDuration()})
    })

    if 0 != len(registrar.names) {
        t.Fatalf("expected nothing registered under a refused configuration, got %v", registrar.names)
    }
}
