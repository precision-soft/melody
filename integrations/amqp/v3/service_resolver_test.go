package amqp

import (
    "context"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
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

/* containerRegistrar hands RegisterService to a real container, as the application's registrar does */
type containerRegistrar struct {
    serviceContainer containercontract.Container
}

func (instance *containerRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.serviceContainer.MustRegister(serviceName, provider, options...)
}

func TestRegisterConnectionService_TheContainerClosesTheConnectionUnderItsDeadlineWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    serviceContainer := container.NewContainer()
    RegisterConnectionService(&containerRegistrar{serviceContainer: serviceContainer}, wedge.connection)

    if wedge.connection != ConnectionMustFromContainer(serviceContainer) {
        t.Fatal("expected the registered connection resolved as itself")
    }

    deadline := 300 * time.Millisecond
    closeContext, cancel := context.WithTimeout(context.Background(), deadline)
    defer cancel()

    closed := make(chan error, 1)
    go func() {
        closed <- serviceContainer.(containercontract.ContextCloser).CloseWithContext(closeContext)
    }()

    select {
    case closeErr := <-closed:
        if nil == closeErr || false == strings.Contains(closeErr.Error(), "failed to close container services") {
            t.Fatalf("expected the close that did not return reported as a failed close, got %v", closeErr)
        }
    case <-time.After(deadline + 2*time.Second):
        t.Fatalf("the container's close did not return within the deadline %s plus two seconds; the connection is closed through its plain Close", deadline)
    }
}
