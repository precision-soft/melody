package messagebus

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
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
    command.hydrateFromContainer(runtimeInstance)

    if consumeBus != command.bus {
        t.Fatalf("expected the consume bus to be hydrated from the container")
    }
    if transport != command.transports["async"] {
        t.Fatalf("expected the transports map to be hydrated from the container")
    }
    if 7 != command.retryPolicy.MaxRetries {
        t.Fatalf("expected the retry policy to be hydrated from the container, got MaxRetries=%d", command.retryPolicy.MaxRetries)
    }
    if 0 >= command.retryPolicy.MaxDelay {
        t.Fatalf("expected the hydrated retry policy to be normalized (MaxDelay > 0), got %v", command.retryPolicy.MaxDelay)
    }

    if consumeErr := command.consumeFrom(runtimeInstance, command.transports["async"], 1, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }
    if 5 != sum {
        t.Fatalf("expected the hydrated command to handle the message (sum 5), got %d", sum)
    }
}
