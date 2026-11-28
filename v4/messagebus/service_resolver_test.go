package messagebus

import (
    "context"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type transportRegistrarAdapter struct {
    serviceContainer containercontract.Container
}

func (instance transportRegistrarAdapter) RegisterService(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) {
    instance.serviceContainer.MustRegister(serviceName, provider, options)
}

func TestConsumeBusFromResolver_PrefersDedicatedConsumeBus(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    dispatchBus := NewManager("dispatch", nil)
    consumeBus := NewManager("consume", nil)

    serviceContainer.MustRegister(ServiceBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return dispatchBus, nil
    }, container.RegisterOptions{})
    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return consumeBus, nil
    }, container.RegisterOptions{TypeRegistrationDisabled: true})

    if consumeBus != ConsumeBusFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the dedicated consume bus to win over the shared bus")
    }
}

func TestConsumeBusFromResolver_FallsBackToSharedBus(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    dispatchBus := NewManager("dispatch", nil)
    serviceContainer.MustRegister(ServiceBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return dispatchBus, nil
    }, container.RegisterOptions{})

    if dispatchBus != ConsumeBusFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected fallback to the shared bus when no dedicated consume bus is registered")
    }
}

func TestNewConsumeCommand_ResolvesTheBusAndTransportsAtRunAndKeepsTheConfigsRetryPolicy(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 5}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    locator := NewHandlerLocator()
    var sum int
    RegisterHandler(locator, func(_ containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        sum += message.Value

        return nil
    })
    consumeBus := NewManager("consume", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleMiddlewareConfig{})})

    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return consumeBus, nil
    }, container.RegisterOptions{})
    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": transport},
    )

    command := NewConsumeCommand(ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(7), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})
    session := mustConsumeSession(command, runtimeInstance)

    if consumeBus != session.bus {
        t.Fatalf("expected the consume bus to be resolved from the container")
    }
    if transport != session.transports["async"] {
        t.Fatalf("expected the transports map to be resolved from the container")
    }
    if 7 != session.retryPolicy.MaxRetries {
        t.Fatalf("expected the config's retry policy, got MaxRetries=%d", session.retryPolicy.MaxRetries)
    }
    if 0 >= session.retryPolicy.Backoff.Max {
        t.Fatalf("expected the resolved retry policy to be normalized (Backoff.Max > 0), got %v", session.retryPolicy.Backoff.Max)
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

func (instance *recordingCloseTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return nil, nil
}

func (instance *recordingCloseTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingCloseTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, options messagebuscontract.NackOptions) error {
    return nil
}

func (instance *recordingCloseTransport) CloseWithContext(closeContext context.Context) error {
    instance.closed.Store(true)
    return nil
}

func TestRegisterTransports_TransportsJoinTheContainerTeardown(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    transport := &recordingCloseTransport{}

    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": transport},
    )

    resolved := TransportsMustFromResolver(t.Context(), serviceContainer)
    if transport != resolved["async"] {
        t.Fatalf("expected the registered transport to resolve")
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
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

    closeErr := closer.CloseWithContext(t.Context())
    if nil == closeErr {
        t.Fatalf("expected the failing transport's close error to surface")
    }

    if logContext := exception.LogContext(closeErr, exception.LogContextOptions{}); "failing" != logContext["transport"] {
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

func (instance *closeFailingTransport) CloseWithContext(closeContext context.Context) error {
    return exception.NewError("broker unreachable", nil, nil)
}

func transportsNamedIn(closeErr error) map[string]bool {
    named := map[string]bool{}

    joined, isJoined := closeErr.(interface{ Unwrap() []error })
    if false == isJoined {
        if transport, hasTransport := exception.LogContext(closeErr, exception.LogContextOptions{})["transport"].(string); true == hasTransport {
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

func (instance *closePanickingTransport) CloseWithContext(closeContext context.Context) error {
    panic(exception.NewError("the broker connection was already torn down", map[string]any{"queue": "orders"}, nil))
}

/* a nil pointer inside a non-nil interface, as a conditionally built transport produces */
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

    closeErr := closer.CloseWithContext(t.Context())
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

    closeErr := closer.CloseWithContext(t.Context())
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

func TestTransportsCloser_APanickingCloseCarriesTheStackOfThePanic(t *testing.T) {
    closer := &TransportsCloser{transports: map[string]messagebuscontract.Transport{
        "a-panicking": &closePanickingTransport{},
    }}

    closeErr := closer.closeOne(context.Background(), "a-panicking")
    if nil == closeErr {
        t.Fatalf("expected the panicking close to surface as a failure")
    }

    panicStack, isString := exception.LogContext(closeErr, exception.LogContextOptions{})["panicStack"].(string)
    if false == isString || false == strings.Contains(panicStack, "closePanickingTransport") {
        t.Fatalf("expected the stack of the panic, through the transport's own Close, got %q", panicStack)
    }
}

func TestRegisterTransports_RefusesANilOrTypedNilEntryByName(t *testing.T) {
    for name, transport := range map[string]messagebuscontract.Transport{
        "untyped": nil,
        "typed":   (*typedNilTransport)(nil),
    } {
        func() {
            defer func() {
                recovered := recover()
                if nil == recovered || false == strings.Contains(exception.LogContext(recovered.(error), exception.LogContextOptions{})["error"].(string), "messagebus transport is nil") || name != exception.LogContext(recovered.(error), exception.LogContextOptions{})["name"] {
                    t.Fatalf("%s: expected the nil entry refused by name at registration, got %v", name, recovered)
                }
            }()

            RegisterTransports(
                transportRegistrarAdapter{serviceContainer: container.NewContainer(container.ContainerConfig{})},
                map[string]messagebuscontract.Transport{name: transport},
            )
        }()
    }
}

type typedNilErrorOnCloseTransport struct {
    recordingCloseTransport
}

func (instance *typedNilErrorOnCloseTransport) CloseWithContext(closeContext context.Context) error {
    _ = instance.recordingCloseTransport.CloseWithContext(context.Background())

    var refusal *exception.Error

    return refusal
}

func TestTransportsCloser_ATypedNilCloseErrorIsReadAsAClose(t *testing.T) {
    transport := &typedNilErrorOnCloseTransport{}

    closer := &TransportsCloser{transports: map[string]messagebuscontract.Transport{"async": transport}}

    if closeErr := closer.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the typed nil read as a close that succeeded, got %v", closeErr)
    }

    if false == transport.closed.Load() {
        t.Fatalf("expected the transport's Close to have run")
    }
}
