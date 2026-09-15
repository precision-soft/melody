package messagebus

import (
    "context"
    "strings"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type transportRegistrarAdapter struct {
    serviceContainer containercontract.Container
}

func (instance transportRegistrarAdapter) RegisterService(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    instance.serviceContainer.MustRegister(serviceName, provider, options...)
}

func TestConsumeBusFromResolver_PrefersDedicatedConsumeBus(t *testing.T) {
    serviceContainer := container.NewContainer()

    dispatchBus := NewManager("dispatch")
    consumeBus := NewManager("consume")

    serviceContainer.MustRegister(ServiceBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return dispatchBus, nil
    })
    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return consumeBus, nil
    }, container.WithoutTypeRegistration())

    if consumeBus != ConsumeBusFromResolver(serviceContainer) {
        t.Fatalf("expected the dedicated consume bus to win over the shared bus")
    }
}

func TestConsumeBusFromResolver_FallsBackToSharedBus(t *testing.T) {
    serviceContainer := container.NewContainer()

    dispatchBus := NewManager("dispatch")
    serviceContainer.MustRegister(ServiceBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return dispatchBus, nil
    })

    if dispatchBus != ConsumeBusFromResolver(serviceContainer) {
        t.Fatalf("expected fallback to the shared bus when no dedicated consume bus is registered")
    }
}

func TestRetryPolicyFromResolver_AbsentThenPresent(t *testing.T) {
    serviceContainer := container.NewContainer()

    if _, hasPolicy := RetryPolicyFromResolver(serviceContainer); true == hasPolicy {
        t.Fatalf("expected no retry policy before one is registered")
    }

    serviceContainer.MustRegister(ServiceRetryPolicy, func(containercontract.Resolver) (RetryPolicy, error) {
        return RetryPolicy{MaxRetries: 9}, nil
    })

    resolvedPolicy, hasPolicy := RetryPolicyFromResolver(serviceContainer)
    if false == hasPolicy || 9 != resolvedPolicy.MaxRetries {
        t.Fatalf("expected the registered retry policy, got %+v (present=%v)", resolvedPolicy, hasPolicy)
    }
}

func TestNewConsumeCommandFromContainer_HydratesBusTransportsAndRetryThenConsumes(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    transport := NewInMemoryTransport(8)
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 5})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    locator := NewHandlerLocator()
    var sum int
    RegisterHandler(locator, func(_ runtimecontract.Runtime, message consumeTestMessage) error {
        sum += message.Value

        return nil
    })
    consumeBus := NewManager("consume", NewHandleMessageMiddleware(locator))

    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return consumeBus, nil
    })
    serviceContainer.MustRegister(ServiceRetryPolicy, func(containercontract.Resolver) (RetryPolicy, error) {
        return RetryPolicy{MaxRetries: 7}, nil
    })
    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": transport},
    )

    command := NewConsumeCommandFromContainer()
    session := command.newConsumeSession(runtimeInstance)

    if consumeBus != session.bus {
        t.Fatalf("expected the consume bus to be resolved from the container")
    }
    if transport != session.transports["async"] {
        t.Fatalf("expected the transports map to be resolved from the container")
    }
    if 7 != session.retryPolicy.MaxRetries {
        t.Fatalf("expected the retry policy to be resolved from the container, got MaxRetries=%d", session.retryPolicy.MaxRetries)
    }
    if 0 >= session.retryPolicy.MaxDelay {
        t.Fatalf("expected the resolved retry policy to be normalized (MaxDelay > 0), got %v", session.retryPolicy.MaxDelay)
    }

    if command.resolveFromContainer {
        if nil != command.bus || nil != command.transports {
            t.Fatalf("expected the container resolution to leave the command instance unmutated")
        }
    }

    if consumeErr := session.consumeFrom(runtimeInstance, session.transports["async"], 1, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }
    if 5 != sum {
        t.Fatalf("expected the hydrated command to handle the message (sum 5), got %d", sum)
    }
}

type recordingCloseTransport struct {
    closed atomic.Bool
}

func (instance *recordingCloseTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Receive(runtimeInstance runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *recordingCloseTransport) Ack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Nack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope, requeue bool) error {
    return nil
}

func (instance *recordingCloseTransport) Close() error {
    instance.closed.Store(true)
    return nil
}

func TestRegisterTransports_TransportsJoinTheContainerTeardown(t *testing.T) {
    serviceContainer := container.NewContainer()

    transport := &recordingCloseTransport{}

    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": transport},
    )

    resolved := TransportsMustFromResolver(serviceContainer)
    if transport != resolved["async"] {
        t.Fatalf("expected the registered transport to resolve")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected container close error: %v", closeErr)
    }

    if false == transport.closed.Load() {
        t.Fatalf("expected the container teardown to close the registered transport")
    }
}

func TestTransportsCloser_ClosesEveryTransportAndJoinsFailures(t *testing.T) {
    healthy := &recordingCloseTransport{}

    closer := &TransportsCloser{transports: map[string]messagebuscontract.Transport{
        "healthy": healthy,
        "failing": &closeFailingTransport{},
    }}

    closeErr := closer.Close()
    if nil == closeErr {
        t.Fatalf("expected the failing transport's close error to surface")
    }

    if logContext := exception.LogContext(closeErr); "failing" != logContext["transport"] {
        t.Fatalf("expected the close error to name the failing transport, got context %v", logContext)
    }

    if false == strings.Contains(closeErr.Error(), "transport close failed") {
        t.Fatalf("expected the close error to say what failed, got %v", closeErr)
    }

    if false == healthy.closed.Load() {
        t.Fatalf("expected the healthy transport to be closed despite the sibling failure")
    }
}

type closeFailingTransport struct {
    recordingCloseTransport
}

func (instance *closeFailingTransport) Close() error {
    return exception.NewError("broker unreachable", nil, nil)
}

func transportsNamedIn(closeErr error) map[string]bool {
    named := map[string]bool{}

    joined, isJoined := closeErr.(interface{ Unwrap() []error })
    if false == isJoined {
        if transport, hasTransport := exception.LogContext(closeErr)["transport"].(string); true == hasTransport {
            named[transport] = true
        }

        return named
    }

    for _, branch := range joined.Unwrap() {
        for transport := range transportsNamedIn(branch) {
            named[transport] = true
        }
    }

    return named
}

type closePanickingTransport struct {
    recordingCloseTransport
}

func (instance *closePanickingTransport) Close() error {
    panic(exception.NewError("the broker connection was already torn down", map[string]any{"queue": "orders"}, nil))
}

type typedNilTransport struct {
    recordingCloseTransport
}

func TestTransportsCloser_ANilTransportDoesNotStrandTheOthers(t *testing.T) {
    healthy := &recordingCloseTransport{}

    closer := &TransportsCloser{transports: map[string]messagebuscontract.Transport{
        "a-untyped-nil": nil,
        "b-typed-nil":   (*typedNilTransport)(nil),
        "z-healthy":     healthy,
    }}

    closeErr := closer.Close()
    if nil == closeErr {
        t.Fatalf("expected the nil transports to be reported rather than closed in silence")
    }

    named := transportsNamedIn(closeErr)

    if false == named["a-untyped-nil"] {
        t.Fatalf("expected the report to name the untyped nil entry, got %v", named)
    }

    if false == named["b-typed-nil"] {
        t.Fatalf("expected the report to name the typed nil entry, got %v", named)
    }

    if false == healthy.closed.Load() {
        t.Fatalf("expected the transport sorted after the nil entries to be closed")
    }

    if 2 != strings.Count(closeErr.Error(), "transport is nil and was not closed") {
        t.Fatalf("expected both nil entries to be named as nil rather than as a panic, got %v", closeErr)
    }
}

func TestTransportsCloser_APanickingCloseDoesNotStrandTheOthers(t *testing.T) {
    healthy := &recordingCloseTransport{}

    closer := &TransportsCloser{transports: map[string]messagebuscontract.Transport{
        "a-panicking": &closePanickingTransport{},
        "z-healthy":   healthy,
    }}

    closeErr := closer.Close()
    if nil == closeErr {
        t.Fatalf("expected the panicking close to surface as a failure")
    }

    if false == transportsNamedIn(closeErr)["a-panicking"] {
        t.Fatalf("expected the report to name the panicking transport, got %v", closeErr)
    }

    if false == healthy.closed.Load() {
        t.Fatalf("expected the transport sorted after the panicking one to be closed")
    }

    causeChain := exception.BuildCauseChain(closeErr, 8)
    if false == strings.Contains(strings.Join(causeChain, " | "), "the broker connection was already torn down") {
        t.Fatalf("expected the panic value to travel as the cause, got chain %v", causeChain)
    }
}
