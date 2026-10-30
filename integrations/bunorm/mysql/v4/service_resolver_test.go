package mysql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

type recordingRegistrar struct {
    names     []string
    providers []any
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
    instance.providers = append(instance.providers, provider)
}

func TestRegisterLockerServiceUsesCoreLockerName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterLockerService(registrar, nil, LockerConfig{})

    if 0 == len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}

func TestRegisterLockerService_TheRegisteredLockerCarriesTheConfiguredReleaseTimeout(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterLockerService(registrar, newReleaseRecordingDatabase(t, &releaseRecordingConnector{}), LockerConfig{ReleaseTimeout: bound.Of(2 * time.Second)})

    provider, isProvider := registrar.providers[0].(func(containercontract.Resolver) (lockcontract.Locker, error))
    if false == isProvider {
        t.Fatalf("expected a locker provider, got %T", registrar.providers[0])
    }

    locker, lockerErr := provider(nil)
    if nil != lockerErr || 2*time.Second != locker.(*Locker).releaseTimeout {
        t.Fatalf("expected the configured release timeout on the registered locker, got %v %v", locker, lockerErr)
    }
}

func TestRegisterLockerService_RefusesAReleaseTimeoutOutsideItsRuleAtTheCall(t *testing.T) {
    registrar := &recordingRegistrar{}

    expectPanicNamingField(t, "LockerConfig.ReleaseTimeout", func() {
        RegisterLockerService(registrar, nil, LockerConfig{ReleaseTimeout: bound.UnlimitedDuration()})
    })

    if 0 != len(registrar.names) {
        t.Fatalf("expected nothing registered under a refused configuration, got %v", registrar.names)
    }
}
