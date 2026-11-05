package amqp

import (
    "context"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type recordingRegistrar struct {
    names   []string
    options []containercontract.RegisterOptions
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
    instance.options = append(instance.options, options)
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

    RegisterConnectionService(registrar, nil, ConnectionServiceConfig{})

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

func TestRegisterConnectionAndTransportService_AreNotWalkedAndTheTransportClosesBeforeTheConnection(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterConnectionService(registrar, nil, ConnectionServiceConfig{})
    RegisterTransportService(registrar, "async", nil)

    if false == registrar.options[0].TeardownReflectionDisabled || false == registrar.options[1].TeardownReflectionDisabled {
        t.Fatalf("expected neither the connection nor the transport walked, got %+v", registrar.options)
    }

    if 1 != len(registrar.options[1].OptionalTeardownDependencyNames) || ServiceConnection != registrar.options[1].OptionalTeardownDependencyNames[0] {
        t.Fatalf("expected the transport ordered before the connection, got %v", registrar.options[1].OptionalTeardownDependencyNames)
    }
}

/* containerRegistrar hands RegisterService to a real container, as the application's registrar does */
type containerRegistrar struct {
    serviceContainer containercontract.Container
}

func (instance *containerRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.serviceContainer.MustRegister(serviceName, provider, options)
}

func TestRegisterConnectionService_TheContainerClosesTheConnectionUnderItsDeadlineWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    RegisterConnectionService(&containerRegistrar{serviceContainer: serviceContainer}, wedge.connection, ConnectionServiceConfig{})

    if wedge.connection != ConnectionMustFromContainer(t.Context(), serviceContainer) {
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

func TestRegisterConnectionService_RefusesACloseTimeoutItsRuleRefusesNamingTheField(t *testing.T) {
    for name, limit := range map[string]bound.Duration{"below the minimum": bound.Of(time.Microsecond), "unlimited": bound.UnlimitedDuration()} {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recovered, isException := recover().(*exception.Error)
                if false == isException || "ConnectionServiceConfig.CloseTimeout" != recovered.Context()["field"] {
                    t.Fatalf("expected a refusal naming the field, got %v", recovered)
                }
            }()

            RegisterConnectionService(&containerRegistrar{serviceContainer: container.NewContainer(container.ContainerConfig{})}, nil, ConnectionServiceConfig{CloseTimeout: limit})
        })
    }
}

func TestRegisterConnectionService_TheConfiguredCloseTimeoutBoundsTheStretchUnderNoDeadline(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    RegisterConnectionService(&containerRegistrar{serviceContainer: serviceContainer}, wedge.connection, ConnectionServiceConfig{CloseTimeout: bound.Of(100 * time.Millisecond)})
    ConnectionMustFromContainer(t.Context(), serviceContainer)

    closed := make(chan error, 1)
    go func() {
        closed <- serviceContainer.(containercontract.ContextCloser).CloseWithContext(context.Background())
    }()

    select {
    case <-closed:
    case <-time.After(5 * time.Second):
        t.Fatal("the close under no deadline must end within the configured close timeout, not the 30s default")
    }
}
