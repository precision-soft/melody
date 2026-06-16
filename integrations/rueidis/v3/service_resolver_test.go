package rueidis

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

func (instance *recordingRegistrar) has(serviceName string) bool {
    for _, name := range instance.names {
        if serviceName == name {
            return true
        }
    }

    return false
}

func TestRegisterClientServiceUsesClientName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterClientService(registrar, nil)

    if false == registrar.has(ServiceClient) {
        t.Fatalf("expected %q to be registered, got %v", ServiceClient, registrar.names)
    }
}

func TestRegisterLockerServiceUsesCoreLockerName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterLockerService(registrar, nil)

    if false == registrar.has(melodylock.ServiceLocker) {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}

func TestRegisterTokenStoreServiceUsesTokenStoreName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterTokenStoreService(registrar, nil)

    if false == registrar.has(ServiceTokenStore) {
        t.Fatalf("expected %q to be registered, got %v", ServiceTokenStore, registrar.names)
    }
}
