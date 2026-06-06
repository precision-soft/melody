package rueidis_test

import (
    "testing"

    rueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
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

    rueidis.RegisterClientService(registrar, nil)

    if false == registrar.has(rueidis.ServiceClient) {
        t.Fatalf("expected %q to be registered, got %v", rueidis.ServiceClient, registrar.names)
    }
}

func TestRegisterLockerServiceUsesCoreLockerName(t *testing.T) {
    registrar := &recordingRegistrar{}

    rueidis.RegisterLockerService(registrar, nil)

    if false == registrar.has(melodylock.ServiceLocker) {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}

func TestRegisterTokenStoreServiceUsesTokenStoreName(t *testing.T) {
    registrar := &recordingRegistrar{}

    rueidis.RegisterTokenStoreService(registrar, nil)

    if false == registrar.has(rueidis.ServiceTokenStore) {
        t.Fatalf("expected %q to be registered, got %v", rueidis.ServiceTokenStore, registrar.names)
    }
}
