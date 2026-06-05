package messagebus

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type consumeTestMessage struct {
    Value int
}

func TestConsumeFrom_StopsAtLimitAndHandlesMessages(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    transport := NewInMemoryTransport(8)

    firstSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}))
    secondSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 2}))
    if nil != firstSendErr || nil != secondSendErr {
        t.Fatalf("unexpected send error: %v %v", firstSendErr, secondSendErr)
    }

    locator := NewHandlerLocator()
    var sum int
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        sum += message.Value
        return nil
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))
    command := NewConsumeCommand(bus, nil)

    consumeErr := command.consumeFrom(runtimeInstance, transport, 2)
    if nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != sum {
        t.Fatalf("expected handlers to sum to 3, got %d", sum)
    }
}

func TestConsume_ExhaustsRetriesAndRoutesToFailureTransport(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    source := NewInMemoryTransport(8)
    failure := NewInMemoryTransport(8)

    if sendErr := source.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 7})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        attempts++
        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))
    command := NewConsumeCommandWithRetry(bus, nil, RetryPolicy{MaxRetries: 2, FailureTransport: failure})

    if consumeErr := command.consumeFrom(runtimeInstance, source, 3); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != attempts {
        t.Fatalf("expected exactly three handling attempts, got %d", attempts)
    }

    failureQueue, _ := failure.Receive(runtimeInstance)
    select {
    case deadLettered := <-failureQueue:
        if 2 != RedeliveryCount(deadLettered) {
            t.Fatalf("expected the dead-lettered envelope to carry a redelivery count of 2, got %d", RedeliveryCount(deadLettered))
        }
    default:
        t.Fatalf("expected the exhausted message to be routed to the failure transport")
    }
}

func TestConsume_ExhaustedWithoutFailureTransportDropsMessage(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    source := NewInMemoryTransport(8)

    if sendErr := source.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 9})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        attempts++
        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))
    command := NewConsumeCommandWithRetry(bus, nil, RetryPolicy{MaxRetries: 2})

    if consumeErr := command.consumeFrom(runtimeInstance, source, 3); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != attempts {
        t.Fatalf("expected exactly three handling attempts, got %d", attempts)
    }

    sourceQueue, _ := source.Receive(runtimeInstance)
    select {
    case leftover := <-sourceQueue:
        t.Fatalf("expected the exhausted message to be dropped, found %v still queued", leftover.Message())
    default:
    }
}
