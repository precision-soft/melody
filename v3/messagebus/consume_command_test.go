package messagebus

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type closedQueueTransport struct {
    queue chan messagebuscontract.Envelope
}

func (instance *closedQueueTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *closedQueueTransport) Receive(runtimeInstance runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return instance.queue, nil
}

func (instance *closedQueueTransport) Ack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *closedQueueTransport) Nack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope, requeue bool) error {
    return nil
}

func (instance *closedQueueTransport) Close(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

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

    consumeErr := command.consumeFrom(runtimeInstance, transport, 2, 1)
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

    if consumeErr := command.consumeFrom(runtimeInstance, source, 3, 1); nil != consumeErr {
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

    if consumeErr := command.consumeFrom(runtimeInstance, source, 3, 1); nil != consumeErr {
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

func TestConsumeFrom_AbnormalChannelCloseReturnsError(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    queue := make(chan messagebuscontract.Envelope)
    close(queue)

    bus := NewManager("default", NewHandleMessageMiddleware(NewHandlerLocator()))
    command := NewConsumeCommand(bus, nil)

    consumeErr := command.consumeFrom(runtimeInstance, &closedQueueTransport{queue: queue}, 0, 1)
    if nil == consumeErr {
        t.Fatalf("expected an error when the delivery channel closes without a cancelled context")
    }
}

func TestRetryDelay_IsCappedAndOverflowSafe(t *testing.T) {
    capped := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 5, BaseDelay: time.Hour})
    if maxRetryDelay != capped.retryDelay(100) {
        t.Fatalf("expected the linear delay to be capped at %v, got %v", maxRetryDelay, capped.retryDelay(100))
    }

    huge := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 5, BaseDelay: time.Duration(1) << 60})
    delay := huge.retryDelay(64)
    if 0 > delay || delay > maxRetryDelay {
        t.Fatalf("expected a non-negative, capped delay, got %v", delay)
    }

    none := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 3})
    if 0 != none.retryDelay(2) {
        t.Fatalf("expected no delay when BaseDelay is zero, got %v", none.retryDelay(2))
    }
}

func TestFailureRequeueDelay_NeverZero(t *testing.T) {
    withoutBase := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 3})
    if defaultFailureRequeueDelay != withoutBase.failureRequeueDelay() {
        t.Fatalf("expected the default failure backoff, got %v", withoutBase.failureRequeueDelay())
    }

    withBase := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 2, BaseDelay: time.Second})
    if 0 >= withBase.failureRequeueDelay() {
        t.Fatalf("expected a positive failure backoff, got %v", withBase.failureRequeueDelay())
    }
}
