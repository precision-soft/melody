package mysql

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func TestRegisterLockerServiceUsesCoreLockerName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterLockerService(registrar, nil)

    if 0 == len(registrar.names) || melodylock.ServiceLocker != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}
