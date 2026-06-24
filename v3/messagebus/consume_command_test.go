package messagebus

import (
    "context"
    "strings"
    "sync/atomic"
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

func TestConsumeFrom_DoesNotOvershootLimitWithConcurrency(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    transport := NewInMemoryTransport(16)

    for value := 0; value < 8; value++ {
        if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1})); nil != sendErr {
            t.Fatalf("unexpected send error: %v", sendErr)
        }
    }

    locator := NewHandlerLocator()
    var handled int64
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        atomic.AddInt64(&handled, 1)
        return nil
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))
    command := NewConsumeCommand(bus, nil)

    consumeErr := command.consumeFrom(runtimeInstance, transport, 2, 8)
    if nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 2 != atomic.LoadInt64(&handled) {
        t.Fatalf("expected exactly 2 messages handled with limit 2 and concurrency 8, got %d", atomic.LoadInt64(&handled))
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

/* @info configurable dead-letter bound: requeue forever by default, give up after N when set (CR #66) */

type recordingNackTransport struct {
    nackCount    int
    nackRequeue  bool
    nackEnvelope messagebuscontract.Envelope
}

func (instance *recordingNackTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingNackTransport) Receive(runtimeInstance runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *recordingNackTransport) Ack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingNackTransport) Nack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope, requeue bool) error {
    instance.nackCount++
    instance.nackRequeue = requeue
    instance.nackEnvelope = envelope
    return nil
}

func (instance *recordingNackTransport) Close(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

type alwaysFailingTransport struct{}

func (instance *alwaysFailingTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return exception.NewError("failure transport is down", nil, nil)
}

func (instance *alwaysFailingTransport) Receive(runtimeInstance runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *alwaysFailingTransport) Ack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *alwaysFailingTransport) Nack(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope, requeue bool) error {
    return nil
}

func (instance *alwaysFailingTransport) Close(runtimeInstance runtimecontract.Runtime) error {
    return nil
}

func newAlwaysFailingConsumeCommand(t *testing.T, policy RetryPolicy) (*ConsumeCommand, runtimecontract.Runtime) {
    t.Helper()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        return exception.NewError("handler always fails", nil, nil)
    })

    return NewConsumeCommandWithRetry(NewManager("default", NewHandleMessageMiddleware(locator)), nil, policy), runtimeInstance
}

func TestConsume_BoundedDeadLetterStopsRequeueAfterMax(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:            0,
        FailureTransport:      &alwaysFailingTransport{},
        MaxDeadLetterAttempts: 2,
    })

    source := &recordingNackTransport{}

    /* @important still under the bound: the failed dead-letter routing must requeue and bump the attempt counter */
    command.consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}).WithStamp(DeadLetterAttemptStamp{Count: 0}))
    if 1 != source.nackCount || false == source.nackRequeue {
        t.Fatalf("expected the first failed dead-letter routing to requeue, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
    if 1 != DeadLetterAttemptCount(source.nackEnvelope) {
        t.Fatalf("expected the requeued envelope to carry dead-letter attempt 1, got %d", DeadLetterAttemptCount(source.nackEnvelope))
    }

    /* @important at the bound: stop requeueing and nack without requeue so a transport-native dead-letter can claim it instead of looping forever */
    source.nackCount = 0
    command.consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}).WithStamp(DeadLetterAttemptStamp{Count: 1}))
    if 1 != source.nackCount || true == source.nackRequeue {
        t.Fatalf("expected a nack without requeue once the dead-letter bound is reached, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

func TestConsume_UnboundedDeadLetterRequeuesByDefault(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:       0,
        FailureTransport: &alwaysFailingTransport{},
    })

    source := &recordingNackTransport{}

    /* @important the default policy (MaxDeadLetterAttempts 0) keeps requeueing regardless of how many attempts have accrued, preserving the documented no-loss behavior */
    command.consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}).WithStamp(DeadLetterAttemptStamp{Count: 99}))
    if 1 != source.nackCount || false == source.nackRequeue {
        t.Fatalf("expected the default policy to keep requeueing (no message loss), got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

func TestNewConsumeCommandWithRetry_ClampsNegativeMaxDeadLetterAttempts(t *testing.T) {
    command := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 1, MaxDeadLetterAttempts: -5})
    if 0 != command.retryPolicy.MaxDeadLetterAttempts {
        t.Fatalf("expected a negative MaxDeadLetterAttempts to clamp to 0 (unbounded), got %d", command.retryPolicy.MaxDeadLetterAttempts)
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

func TestConsumeFrom_ShutdownGraceTimesOutWedgedHandler(t *testing.T) {
    serviceContainer := container.NewContainer()
    consumeContext, cancel := context.WithCancel(context.Background())
    defer cancel()
    runtimeInstance := runtime.New(consumeContext, serviceContainer.NewScope(), serviceContainer)

    transport := NewInMemoryTransport(1)
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    started := make(chan struct{})
    release := make(chan struct{})
    defer close(release)

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        close(started)
        <-release
        return nil
    })
    bus := NewManager("default", NewHandleMessageMiddleware(locator))

    command := NewConsumeCommand(bus, nil).WithShutdownGrace(50 * time.Millisecond)

    done := make(chan error, 1)
    go func() {
        done <- command.consumeFrom(runtimeInstance, transport, 0, 1)
    }()

    select {
    case <-started:
    case <-time.After(2 * time.Second):
        t.Fatalf("the handler never started")
    }

    cancel()

    select {
    case consumeErr := <-done:
        if nil == consumeErr {
            t.Fatalf("expected a shutdown-timeout error for a wedged handler")
        }
        if false == strings.Contains(consumeErr.Error(), "timed out") {
            t.Fatalf("expected a shutdown-timeout error, got: %v", consumeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("consumeFrom did not return within the grace window after the context was cancelled")
    }
}

func TestRetryDelay_IsCappedAndOverflowSafe(t *testing.T) {
    capped := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 5, BaseDelay: time.Hour})
    if defaultMaxRetryDelay != capped.retryDelay(100) {
        t.Fatalf("expected the linear delay to be capped at %v, got %v", defaultMaxRetryDelay, capped.retryDelay(100))
    }

    huge := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 5, BaseDelay: time.Duration(1) << 60})
    delay := huge.retryDelay(64)
    if 0 > delay || delay > defaultMaxRetryDelay {
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

func TestRetryDelay_MaxDelayOverride(t *testing.T) {
    command := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 5, BaseDelay: time.Hour, MaxDelay: 10 * time.Minute})

    if 10*time.Minute != command.retryDelay(100) {
        t.Fatalf("expected the delay to be capped at the overridden MaxDelay 10m, got %v", command.retryDelay(100))
    }
}

func TestFailureRequeueDelay_Override(t *testing.T) {
    command := NewConsumeCommandWithRetry(nil, nil, RetryPolicy{MaxRetries: 3, FailureRequeueDelay: 7 * time.Second})

    if 7*time.Second != command.failureRequeueDelay() {
        t.Fatalf("expected the overridden failure requeue delay 7s, got %v", command.failureRequeueDelay())
    }
}
