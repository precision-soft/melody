package amqp

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

func TestRegisterConnectionServiceUsesConnectionName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterConnectionService(registrar, nil)

    if false == registrar.has(ServiceConnection) {
        t.Fatalf("expected %q to be registered, got %v", ServiceConnection, registrar.names)
    }
}

func TestRegisterTransportServiceUsesGivenName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterTransportService(registrar, "async", nil)

    if false == registrar.has("async") {
        t.Fatalf("expected %q to be registered, got %v", "async", registrar.names)
    }
}
