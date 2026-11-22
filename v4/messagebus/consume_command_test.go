package messagebus

import (
    "errors"
    "context"
    "io"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type closedQueueTransport struct {
    streamErr error
    queue chan messagebuscontract.Envelope
}

func (instance *closedQueueTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *closedQueueTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return &queueSubscription{queue: instance.queue, err: instance.streamErr}, nil
}

func (instance *closedQueueTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *closedQueueTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, config messagebuscontract.NackConfig) error {
    return nil
}

func (instance *closedQueueTransport) CloseWithContext(closeContext context.Context) error {
    return nil
}

type consumeTestMessage struct {
    Value int
}

func TestConsumeFrom_StopsAtLimitAndHandlesMessages(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    firstSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil))
    secondSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 2}, nil))
    if nil != firstSendErr || nil != secondSendErr {
        t.Fatalf("unexpected send error: %v %v", firstSendErr, secondSendErr)
    }

    locator := NewHandlerLocator()
    var sum int
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        sum += message.Value
        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 2, 1)
    if nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != sum {
        t.Fatalf("expected handlers to sum to 3, got %d", sum)
    }
}

func TestConsumeFrom_DoesNotOvershootLimitWithConcurrency(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(16)})

    for value := 0; value < 8; value++ {
        if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil)); nil != sendErr {
            t.Fatalf("unexpected send error: %v", sendErr)
        }
    }

    locator := NewHandlerLocator()
    var handled int64
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        atomic.AddInt64(&handled, 1)
        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 2, 8)
    if nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 2 != atomic.LoadInt64(&handled) {
        t.Fatalf("expected exactly 2 messages handled with limit 2 and concurrency 8, got %d", atomic.LoadInt64(&handled))
    }
}

func TestConsume_ExhaustsRetriesAndRoutesToFailureTransport(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    source := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})
    failure := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    if sendErr := source.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 7}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        attempts++
        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOverTransports(bus, ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(2), FailureTransportName: "failed", Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}}, map[string]messagebuscontract.Transport{"failed": failure})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, source, 3, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != attempts {
        t.Fatalf("expected exactly three handling attempts, got %d", attempts)
    }

    failureQueueSubscription, _ := failure.Receive(runtimeInstance)

    failureQueue := failureQueueSubscription.Envelopes()
    select {
    case deadLettered := <-failureQueue:
        if 2 != RedeliveryCount(deadLettered) {
            t.Fatalf("expected the dead-lettered envelope to carry a redelivery count of 2, got %d", RedeliveryCount(deadLettered))
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the exhausted message to be routed to the failure transport")
    }
}

func TestConsume_AMessageExhaustedOnTheFailureTransportItselfIsNotSentBackToIt(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    failure := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    exhausted := NewEnvelope(consumeTestMessage{Value: 7}, []messagebuscontract.Stamp{RedeliveryStamp{Count: 2}})
    if sendErr := failure.Send(runtimeInstance, exhausted); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        attempts++
        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOverTransports(bus, ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(2), FailureTransportName: "failed", Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}}, map[string]messagebuscontract.Transport{"failed": failure})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, failure, 1, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 1 != attempts {
        t.Fatalf("expected the exhausted message handled once and not cycled through its own transport, got %d attempts", attempts)
    }

    failureQueueSubscription, _ := failure.Receive(runtimeInstance)

    failureQueue := failureQueueSubscription.Envelopes()
    select {
    case leftover := <-failureQueue:
        t.Fatalf("expected the message not sent back to the failure transport, found %v", leftover.Message())
    case <-time.After(100 * time.Millisecond):
    }
}

func TestConsume_ExhaustedWithoutFailureTransportDropsMessage(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    source := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    if sendErr := source.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 9}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        attempts++
        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, source, 3, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != attempts {
        t.Fatalf("expected exactly three handling attempts, got %d", attempts)
    }

    sourceQueueSubscription, _ := source.Receive(runtimeInstance)

    sourceQueue := sourceQueueSubscription.Envelopes()
    select {
    case leftover := <-sourceQueue:
        t.Fatalf("expected the exhausted message to be dropped, found %v still queued", leftover.Message())
    case <-time.After(100 * time.Millisecond):
    }
}

/* configurable dead-letter bound: requeue forever by default, give up after N when set */

type recordingNackTransport struct {
    nackCount    int
    nackRequeue  bool
    nackEnvelope messagebuscontract.Envelope
}

func (instance *recordingNackTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingNackTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return nil, nil
}

func (instance *recordingNackTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingNackTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, config messagebuscontract.NackConfig) error {
    instance.nackCount++
    instance.nackRequeue = config.Requeue
    instance.nackEnvelope = envelope
    return nil
}

func (instance *recordingNackTransport) CloseWithContext(closeContext context.Context) error {
    return nil
}

type alwaysFailingTransport struct{}

func (instance *alwaysFailingTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return exception.NewError("failure transport is down", nil, nil)
}

func (instance *alwaysFailingTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return nil, nil
}

func (instance *alwaysFailingTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *alwaysFailingTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, config messagebuscontract.NackConfig) error {
    return nil
}

func (instance *alwaysFailingTransport) CloseWithContext(closeContext context.Context) error {
    return nil
}

func newAlwaysFailingConsumeCommand(t *testing.T, policy RetryPolicy) (*consumeCommandOverBus, containercontract.Runtime) {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        return exception.NewError("handler always fails", nil, nil)
    })

    return newConsumeCommandOverTransports(NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})}), ConsumeCommandConfig{Retry: policy}, map[string]messagebuscontract.Transport{"failed": &alwaysFailingTransport{}}), runtimeInstance
}

func TestConsume_BoundedDeadLetterStopsRequeueAfterMax(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:            bound.Of(0),
        FailureTransportName:  "failed",
        MaxDeadLetterAttempts: bound.Of(2),
        Backoff:               bound.Backoff{Initial: bound.Of(time.Millisecond)},
    })

    source := &recordingNackTransport{}

    /* still under the bound: the failed dead-letter routing must requeue and bump the attempt counter */
    mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil).WithStamps([]messagebuscontract.Stamp{DeadLetterAttemptStamp{Count: 0}}))
    if 1 != source.nackCount || false == source.nackRequeue {
        t.Fatalf("expected the first failed dead-letter routing to requeue, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
    if 1 != DeadLetterAttemptCount(source.nackEnvelope) {
        t.Fatalf("expected the requeued envelope to carry dead-letter attempt 1, got %d", DeadLetterAttemptCount(source.nackEnvelope))
    }

    /* at the bound: stop requeueing and nack without requeue so a transport-native dead-letter can claim it instead of looping forever */
    source.nackCount = 0
    mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil).WithStamps([]messagebuscontract.Stamp{DeadLetterAttemptStamp{Count: 1}}))
    if 1 != source.nackCount || true == source.nackRequeue {
        t.Fatalf("expected a nack without requeue once the dead-letter bound is reached, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

func TestConsume_UnlimitedDeadLetterAttemptsKeepRequeueing(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:            bound.Of(0),
        FailureTransportName:  "failed",
        Backoff:               bound.Backoff{Initial: bound.Of(time.Millisecond)},
        MaxDeadLetterAttempts: bound.UnlimitedCount(),
    })

    source := &recordingNackTransport{}

    mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil).WithStamps([]messagebuscontract.Stamp{DeadLetterAttemptStamp{Count: 99}}))
    if 1 != source.nackCount || false == source.nackRequeue {
        t.Fatalf("expected an unlimited policy to keep requeueing, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

func TestNewConsumeCommand_AbsentRetryPolicyResolvesTheDefaults(t *testing.T) {
    for name, command := range map[string]*ConsumeCommand{
        "over a bus": newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{}).ConsumeCommand,
        "container":  NewConsumeCommand(ConsumeCommandConfig{}),
    } {
        policy := command.retryPolicy
        if 3 != policy.MaxRetries || (bound.ResolvedBackoff{Initial: time.Second, Max: time.Hour, Factor: 2}) != policy.Backoff || 5*time.Second != policy.FailureRequeueDelay || 5 != policy.MaxDeadLetterAttempts || true == policy.MaxDeadLetterAttemptsUnlimited {
            t.Fatalf("%s: expected the default retry policy, got %+v", name, policy)
        }
        if 30*time.Second != command.shutdownTimeout {
            t.Fatalf("%s: expected the default shutdown timeout 30s, got %v", name, command.shutdownTimeout)
        }
    }
}

func TestNewConsumeCommand_ZeroMaxRetriesIsNoRetry(t *testing.T) {
    command := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(0)}})
    if 0 != command.retryPolicy.MaxRetries {
        t.Fatalf("expected MaxRetries 0 to be no retry, got %d", command.retryPolicy.MaxRetries)
    }
}

func TestNewConsumeCommand_UnlimitedMaxDeadLetterAttemptsIsAdmitted(t *testing.T) {
    command := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxDeadLetterAttempts: bound.UnlimitedCount()}})
    if false == command.retryPolicy.MaxDeadLetterAttemptsUnlimited {
        t.Fatalf("expected an unlimited MaxDeadLetterAttempts to be kept")
    }
}

func TestNewConsumeCommand_RefusesAFieldOutsideItsRuleByName(t *testing.T) {
    cases := map[string]struct {
        config  ConsumeCommandConfig
        message string
        field   string
    }{
        "max retries unlimited":           {ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.UnlimitedCount()}}, "the limit does not admit unlimited", "RetryPolicy.MaxRetries"},
        "max retries negative":            {ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(-1)}}, "the limit is below its minimum", "RetryPolicy.MaxRetries"},
        "initial backoff below 1ms":       {ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Initial: bound.Of(time.Microsecond)}}}, "the limit is below its minimum", "RetryPolicy.Backoff.Initial"},
        "initial backoff unlimited":       {ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Initial: bound.UnlimitedDuration()}}}, "the limit does not admit unlimited", "RetryPolicy.Backoff.Initial"},
        "max backoff below 1ms":           {ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Max: bound.Of(time.Microsecond)}}}, "the limit is below its minimum", "RetryPolicy.Backoff.Max"},
        "max backoff below initial":       {ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Hour)}}}, "the initial backoff exceeds the maximum backoff", "RetryPolicy.Backoff.Initial"},
        "backoff factor below 1":          {ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Factor: 0.5}}}, "the backoff factor is below 1", "RetryPolicy.Backoff.Factor"},
        "failure requeue delay below 1ms": {ConsumeCommandConfig{Retry: RetryPolicy{FailureRequeueDelay: bound.Of(time.Microsecond)}}, "the limit is below its minimum", "RetryPolicy.FailureRequeueDelay"},
        "dead letter attempts zero":       {ConsumeCommandConfig{Retry: RetryPolicy{MaxDeadLetterAttempts: bound.Of(0)}}, "the limit is below its minimum", "RetryPolicy.MaxDeadLetterAttempts"},
        "shutdown timeout below 1ms":      {ConsumeCommandConfig{ShutdownTimeout: bound.Of(time.Microsecond)}, "the limit is below its minimum", "ConsumeCommandConfig.ShutdownTimeout"},
        "shutdown timeout unlimited":      {ConsumeCommandConfig{ShutdownTimeout: bound.UnlimitedDuration()}, "the limit does not admit unlimited", "ConsumeCommandConfig.ShutdownTimeout"},
        "padded failure transport name":   {ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: " failed"}}, "retry policy failure transport name has surrounding whitespace", "RetryPolicy.FailureTransportName"},
        "trailing padded failure name":    {ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: "failed "}}, "retry policy failure transport name has surrounding whitespace", "RetryPolicy.FailureTransportName"},
        "tab padded failure name":         {ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: "\tfailed"}}, "retry policy failure transport name has surrounding whitespace", "RetryPolicy.FailureTransportName"},
        "blank failure name":              {ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: " "}}, "retry policy failure transport name has surrounding whitespace", "RetryPolicy.FailureTransportName"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                NewConsumeCommand(testCase.config)
            }, testCase.message)

            context := recoveredPanicContext(func() {
                NewConsumeCommand(testCase.config)
            })
            if testCase.field != context["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, context)
            }
        })
    }
}

func TestConsume_AbsentDeadLetterAttemptsStopRequeueAtFive(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:           bound.Of(0),
        Backoff:              bound.Backoff{Initial: bound.Of(time.Millisecond)},
        FailureTransportName: "failed",
    })

    source := &recordingNackTransport{}

    mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil).WithStamps([]messagebuscontract.Stamp{DeadLetterAttemptStamp{Count: 3}}))
    if 1 != source.nackCount || false == source.nackRequeue {
        t.Fatalf("expected the fourth failed dead-letter routing to requeue, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }

    source.nackCount = 0
    mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil).WithStamps([]messagebuscontract.Stamp{DeadLetterAttemptStamp{Count: 4}}))
    if 1 != source.nackCount || true == source.nackRequeue {
        t.Fatalf("expected the fifth failed dead-letter routing to nack without requeue, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

func TestConsumeFrom_AbnormalChannelCloseReturnsError(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    queue := make(chan messagebuscontract.Envelope)
    close(queue)

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(NewHandlerLocator(), HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, &closedQueueTransport{queue: queue}, 0, 1)
    if nil == consumeErr {
        t.Fatalf("expected an error when the delivery channel closes without a cancelled context")
    }
}

func TestConsumeFrom_AStreamThatEndsWithAReasonAnswersIt(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    queue := make(chan messagebuscontract.Envelope)
    close(queue)

    streamErr := errors.New("the broker connection is gone")

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(NewHandlerLocator(), HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, &closedQueueTransport{queue: queue, streamErr: streamErr}, 0, 1)
    if false == errors.Is(consumeErr, streamErr) {
        t.Fatalf("expected the stream's reason in the error, got %v", consumeErr)
    }

    if false == strings.Contains(consumeErr.Error(), "transport delivery channel closed") {
        t.Fatalf("expected the error to name the closed channel, got %v", consumeErr)
    }
}

func TestConsumeFrom_AStreamThatEndsAfterTheStopAnswersNilWhateverItsReason(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    consumeContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance := runtime.NewRun(consumeContext, serviceContainer.NewScope())

    queue := make(chan messagebuscontract.Envelope)
    close(queue)

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(NewHandlerLocator(), HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, &closedQueueTransport{queue: queue, streamErr: errors.New("ignored after the stop")}, 0, 1)
    if nil != consumeErr {
        t.Fatalf("expected no error for a stream that ended after the stop, got %v", consumeErr)
    }
}

func TestClosedStreamError_AStreamClosedAfterTheStopIsNoErrorWhateverItsReason(t *testing.T) {
    reason := errors.New("the connection is gone")

    stoppedContext, stop := context.WithCancel(context.Background())
    stop()

    if closedErr := closedStreamError(stoppedContext, &queueSubscription{err: reason}); nil != closedErr {
        t.Fatalf("expected no error for a stream closed after the stop, got %v", closedErr)
    }

    if closedErr := closedStreamError(context.Background(), &queueSubscription{err: reason}); false == errors.Is(closedErr, reason) {
        t.Fatalf("expected the reason as the cause of a closure without the stop, got %v", closedErr)
    }

    if closedErr := closedStreamError(context.Background(), &queueSubscription{}); nil == closedErr || false == strings.Contains(closedErr.Error(), "closed unexpectedly") {
        t.Fatalf("expected a closure with no reason and no stop named unexpected, got %v", closedErr)
    }
}

func TestConsumeFrom_ATransportThatAnswersNoSubscriptionIsRefused(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    bus := NewManager("default", []messagebuscontract.Middleware{})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, &recordingNackTransport{}, 0, 1)
    if nil == consumeErr || false == strings.Contains(consumeErr.Error(), "the transport answered no subscription") {
        t.Fatalf("expected the absent subscription refused, got %v", consumeErr)
    }
}

func TestConsumeFrom_ShutdownTimeoutTimesOutWedgedHandler(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    consumeContext, cancel := context.WithCancel(context.Background())
    defer cancel()
    runtimeInstance := runtime.NewRun(consumeContext, serviceContainer.NewScope())

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(1)})
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    started := make(chan struct{})
    release := make(chan struct{})
    defer close(release)

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        close(started)
        <-release
        return nil
    })
    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    command := newConsumeCommandOver(bus, ConsumeCommandConfig{ShutdownTimeout: bound.Of(50 * time.Millisecond)})

    done := make(chan error, 1)
    go func() {
        done <- mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 0, 1)
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

type contextAwareTransport struct {
    streamErr error
    queue chan messagebuscontract.Envelope

    ackCalls     int
    ackErr       error
    nackCalls    int
    nackRequeue  bool
    nackEnvelope messagebuscontract.Envelope
    nackErr      error
}

func (instance *contextAwareTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return nil
}

func (instance *contextAwareTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return &queueSubscription{queue: instance.queue, err: instance.streamErr}, nil
}

func (instance *contextAwareTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    instance.ackCalls++
    instance.ackErr = runtimeInstance.Context().Err()

    return instance.ackErr
}

func (instance *contextAwareTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, config messagebuscontract.NackConfig) error {
    instance.nackCalls++
    instance.nackRequeue = config.Requeue
    instance.nackEnvelope = envelope
    instance.nackErr = runtimeInstance.Context().Err()

    return instance.nackErr
}

func (instance *contextAwareTransport) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestConsumeFrom_ShutdownTimeoutKeepsTheHandlerAndAckContextAlive(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    parentContext, cancel := context.WithCancel(context.Background())
    defer cancel()
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    transport := &contextAwareTransport{queue: make(chan messagebuscontract.Envelope, 1)}
    transport.queue <- NewEnvelope(consumeTestMessage{Value: 1}, nil)

    started := make(chan struct{})
    release := make(chan struct{})

    var handlerContextErr error
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(handlerRuntime containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        close(started)
        <-release
        handlerContextErr = handlerRuntime.Context().Err()

        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{ShutdownTimeout: bound.Of(5 * time.Second)})

    done := make(chan error, 1)
    go func() {
        done <- mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 1, 1)
    }()

    select {
    case <-started:
    case <-time.After(2 * time.Second):
        t.Fatalf("the handler never started")
    }

    cancel()
    close(release)

    select {
    case consumeErr := <-done:
        if nil != consumeErr {
            t.Fatalf("unexpected consume error: %v", consumeErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("consumeFrom did not return after the in-flight handler finished")
    }

    if nil != handlerContextErr {
        t.Fatalf("the shutdown signal cancelled the in-flight handler instead of letting the grace cover it: %v", handlerContextErr)
    }

    if 1 != transport.ackCalls {
        t.Fatalf("expected exactly one ack, got %d", transport.ackCalls)
    }

    if nil != transport.ackErr {
        t.Fatalf("the ack of a message whose side effects already committed ran on a cancelled context, so the broker redelivers it: %v", transport.ackErr)
    }
}

func TestConsumeFrom_ShutdownTimeoutKeepsTheNackContextAlive(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    parentContext, cancel := context.WithCancel(context.Background())
    defer cancel()
    runtimeInstance := runtime.NewRun(parentContext, serviceContainer.NewScope())

    transport := &contextAwareTransport{queue: make(chan messagebuscontract.Envelope, 1)}
    transport.queue <- NewEnvelope(consumeTestMessage{Value: 1}, nil)

    started := make(chan struct{})
    release := make(chan struct{})

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(handlerRuntime containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        close(started)
        <-release

        return exception.NewError("handler always fails", nil, nil)
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}, ShutdownTimeout: bound.Of(5 * time.Second)})

    done := make(chan error, 1)
    go func() {
        done <- mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 1, 1)
    }()

    select {
    case <-started:
    case <-time.After(2 * time.Second):
        t.Fatalf("the handler never started")
    }

    cancel()
    close(release)

    select {
    case consumeErr := <-done:
        if nil != consumeErr {
            t.Fatalf("unexpected consume error: %v", consumeErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("consumeFrom did not return after the in-flight handler finished")
    }

    if 1 != transport.nackCalls || false == transport.nackRequeue {
        t.Fatalf("expected exactly one requeueing nack, got nackCalls=%d requeue=%v", transport.nackCalls, transport.nackRequeue)
    }

    if nil != transport.nackErr {
        t.Fatalf("the requeue ran on a cancelled context, so the redelivery count never reaches the broker and MaxRetries never converges: %v", transport.nackErr)
    }

    if 1 != RedeliveryCount(transport.nackEnvelope) {
        t.Fatalf("expected the requeued envelope to carry a redelivery count of 1, got %d", RedeliveryCount(transport.nackEnvelope))
    }
}

func TestRetryDelay_IsCappedAndOverflowSafe(t *testing.T) {
    capped := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(time.Hour)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})
    if retryBackoffRule.MaxDefault != mustConsumeSession(capped, nil).retryDelay(100) {
        t.Fatalf("expected the delay to be capped at %v, got %v", retryBackoffRule.MaxDefault, mustConsumeSession(capped, nil).retryDelay(100))
    }

    huge := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(time.Hour), Factor: 1 << 60}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})
    delay := mustConsumeSession(huge, nil).retryDelay(64)
    if 0 > delay || delay > retryBackoffRule.MaxDefault {
        t.Fatalf("expected a non-negative, capped delay, got %v", delay)
    }

    linear := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(3), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})
    if 2*time.Millisecond != mustConsumeSession(linear, nil).retryDelay(2) {
        t.Fatalf("expected the delay doubled after the first retry, got %v", mustConsumeSession(linear, nil).retryDelay(2))
    }
}

func TestRetryDelay_TheDefaultBackoffDoublesFromOneSecond(t *testing.T) {
    session := mustConsumeSession(newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{}), nil)

    for attempt, expected := range map[int]time.Duration{0: 0, 1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 13: time.Hour} {
        if expected != session.retryDelay(attempt) {
            t.Fatalf("attempt %d: expected %s, got %s", attempt, expected, session.retryDelay(attempt))
        }
    }
}

func TestFailureRequeueDelay_IsTheConfiguredDelayWhateverTheBackoff(t *testing.T) {
    absent := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Initial: bound.Of(time.Hour)}}})
    if retryFailureRequeueDelayRule.Default != mustConsumeSession(absent, nil).failureRequeueDelay() {
        t.Fatalf("expected the default failure requeue delay beside a long backoff, got %v", mustConsumeSession(absent, nil).failureRequeueDelay())
    }

    configured := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{Backoff: bound.Backoff{Initial: bound.Of(time.Second)}, FailureRequeueDelay: bound.Of(time.Minute)}})
    if time.Minute != mustConsumeSession(configured, nil).failureRequeueDelay() {
        t.Fatalf("expected the configured failure requeue delay, got %v", mustConsumeSession(configured, nil).failureRequeueDelay())
    }
}

func TestRetryDelay_MaxBackoffOverride(t *testing.T) {
    command := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(time.Minute), Max: bound.Of(10 * time.Minute)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})

    if 10*time.Minute != mustConsumeSession(command, nil).retryDelay(100) {
        t.Fatalf("expected the delay to be capped at the overridden Backoff.Max 10m, got %v", mustConsumeSession(command, nil).retryDelay(100))
    }
}

func TestFailureRequeueDelay_Override(t *testing.T) {
    command := newConsumeCommandOver(NewManager("test", nil), ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(3), FailureRequeueDelay: bound.Of(7 * time.Second), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}})

    if 7*time.Second != mustConsumeSession(command, nil).failureRequeueDelay() {
        t.Fatalf("expected the overridden failure requeue delay 7s, got %v", mustConsumeSession(command, nil).failureRequeueDelay())
    }
}

func TestConsume_PanickingHandlerFlowsIntoRetryPipeline(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    source := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})
    failure := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    if sendErr := source.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 13}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var attempts int
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        attempts++
        panic("poison message handler")
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOverTransports(bus, ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(2), FailureTransportName: "failed", Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()}}, map[string]messagebuscontract.Transport{"failed": failure})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, source, 3, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != attempts {
        t.Fatalf("expected the panicking handler to be retried like an erroring one (three attempts), got %d", attempts)
    }

    failureQueueSubscription, _ := failure.Receive(runtimeInstance)

    failureQueue := failureQueueSubscription.Envelopes()
    select {
    case deadLettered := <-failureQueue:
        if 2 != RedeliveryCount(deadLettered) {
            t.Fatalf("expected the dead-lettered envelope to carry a redelivery count of 2, got %d", RedeliveryCount(deadLettered))
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the poison message to reach the failure transport instead of crashing the consumer")
    }
}

func TestDispatchSafely_ConvertsErrorPanicIntoDispatchError(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        panic(exception.NewError("handler exploded", nil, nil))
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    dispatchErr := mustConsumeSession(command, runtimeInstance).dispatchSafely(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil))
    if nil == dispatchErr {
        t.Fatalf("expected the panic to surface as a dispatch error")
    }

    if false == strings.Contains(dispatchErr.Error(), "message handler panicked") {
        t.Fatalf("expected the dispatch error to identify the handler panic, got %v", dispatchErr)
    }
}

type consumeNilPanicError struct {
    reason string
}

func (instance *consumeNilPanicError) Error() string {
    return instance.reason
}

/* a handler panicking with a typed-nil error answers no cause, as every recovery boundary reads it, and takes the path of a non-error panic: the dispatch error carries the recovered value and renders, where the typed nil travelled as the cause and its Error dereferenced nil. */
func TestDispatchSafely_ATypedNilPanicIsAnsweredWithoutACause(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        var typedNil *consumeNilPanicError

        panic(typedNil)
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    dispatchErr := mustConsumeSession(command, runtimeInstance).dispatchSafely(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil))
    melodyErr, isMelodyErr := dispatchErr.(*exception.Error)
    if false == isMelodyErr || "message handler panicked" != melodyErr.Message() {
        t.Fatalf("expected the panic to surface as a dispatch error, got %v", dispatchErr)
    }

    if nil != melodyErr.Unwrap() {
        t.Fatalf("expected no cause for a typed-nil panic, got %#v", melodyErr.Unwrap())
    }

    if _, carried := melodyErr.Context()["recoveredValue"]; false == carried {
        t.Fatalf("expected the recovered value carried as for a non-error panic, got %v", melodyErr.Context())
    }

    _ = exception.LogContext(dispatchErr, exception.LogContextConfig{})
}

type typedNilAckPanickingTransport struct {
    recordingNackTransport
}

func (instance *typedNilAckPanickingTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    var typedNil *consumeNilPanicError

    panic(typedNil)
}

type panicRecordCapturingLogger struct {
    messageRecordingLogger
    panicContext loggingcontract.Context
}

func (instance *panicRecordCapturingLogger) Error(message string, context loggingcontract.Context) {
    instance.record(message)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if "message processing panicked outside the handler" == message {
        instance.panicContext = context
    }
}

/* a typed-nil error panicking outside the handler — here from Ack — is logged as a panic without a cause and the worker returns to take the next delivery; the record's context renders no cause for it, the typed nil being read as none. */
func TestConsumeRecovered_ATypedNilPanicOutsideTheHandlerIsLoggedWithoutACause(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    logger := &panicRecordCapturingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})
    runtimeInstance := runtime.NewRun(context.Background(), scope)

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    mustConsumeSession(command, runtimeInstance).consumeRecovered(runtimeInstance, &typedNilAckPanickingTransport{}, NewEnvelope(consumeTestMessage{Value: 1}, nil))

    logger.mutex.Lock()
    defer logger.mutex.Unlock()

    if nil == logger.panicContext {
        t.Fatalf("expected the panic outside the handler logged")
    }

    if _, hasCause := logger.panicContext["cause"]; true == hasCause {
        t.Fatalf("expected no cause for a typed-nil panic, got %v", logger.panicContext)
    }
}

func TestConsume_MessagesGetIsolatedScopes(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 2}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    var leaked int64
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(handlerRuntime containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        /* each delivery must see its own scope: an override written for one message must not be visible while handling another */
        if true == handlerRuntime.Scope().Has("service.ambient") {
            atomic.AddInt64(&leaked, 1)
        }

        handlerRuntime.Scope().MustOverrideProtectedInstance("service.ambient", &consumeTestMessage{Value: message.Value}, container.OverrideOptions{})

        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 2, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 0 != atomic.LoadInt64(&leaked) {
        t.Fatalf("expected scope-based ambient state not to leak across messages, got %d leaks", atomic.LoadInt64(&leaked))
    }
}

type contextCapturingLogger struct {
    mutex    sync.Mutex
    contexts []loggingcontract.Context
}

func (instance *contextCapturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *contextCapturingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *contextCapturingLogger) Info(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    instance.contexts = append(instance.contexts, context)
    instance.mutex.Unlock()
}

func (instance *contextCapturingLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *contextCapturingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *contextCapturingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *contextCapturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *contextCapturingLogger) CauseChainDepth() int {
    return 8
}

func TestConsume_MessageScopeCarriesMessageIdLogger(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    capturing := &contextCapturingLogger{}

    baseScope := serviceContainer.NewScope()
    baseScope.MustOverrideProtectedInstance(logging.ServiceLogger, capturing, container.OverrideOptions{})

    runtimeInstance := runtime.NewRun(context.Background(), baseScope)

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})

    envelope := NewEnvelope(consumeTestMessage{Value: 9}, []messagebuscontract.Stamp{MessageIdStamp{MessageId: "msg-e2e-9"}})
    if sendErr := transport.Send(runtimeInstance, envelope); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(handlerRuntime containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        handlerLogger := logging.LoggerFromRuntime(handlerRuntime)
        if nil != handlerLogger {
            handlerLogger.Info("handled", nil)
        }

        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    command := newConsumeCommandOver(bus, ConsumeCommandConfig{})

    if consumeErr := mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, transport, 1, 1); nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    capturing.mutex.Lock()
    defer capturing.mutex.Unlock()

    if 1 != len(capturing.contexts) {
        t.Fatalf("expected exactly one handler log line, got %d", len(capturing.contexts))
    }

    if "msg-e2e-9" != capturing.contexts[0]["messageId"] {
        t.Fatalf("expected the handler log context to carry the message id, got %v", capturing.contexts[0])
    }
}

/* the in-process cron runner overlaps a run with itself whenever an entry outruns its interval, so two concurrent Runs of the single auto-registered command are a supported deployment shape — before the run-local session, the second Run's container hydration wrote the singleton's bus, transports and retryPolicy fields while the first Run's workers were reading them, a data race the -race band fails on. */
func TestRun_OverlappingRunsShareNoMutableState(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        return nil
    })
    consumeBus := NewManager("consume", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return consumeBus, nil
    }, container.RegisterOptions{})

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})
    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": transport},
    )

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    for message := 0; message < 4; message++ {
        if sendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: message}, nil)); nil != sendErr {
            t.Fatalf("unexpected send error: %v", sendErr)
        }
    }

    command := NewConsumeCommand(ConsumeCommandConfig{})

    var wait sync.WaitGroup
    for overlap := 0; overlap < 2; overlap++ {
        wait.Add(1)
        go func() {
            defer wait.Done()

            session := mustConsumeSession(command, runtimeInstance)
            if runErr := session.consumeFrom(runtimeInstance, session.transports["async"], 2, 2); nil != runErr {
                t.Errorf("unexpected consume error: %v", runErr)
            }
        }()
    }

    wait.Wait()
}

func countStampsNamed(envelopeInstance messagebuscontract.Envelope, stampName string) int {
    count := 0
    for _, stamp := range envelopeInstance.Stamps() {
        if stampName == stamp.StampName() {
            count++
        }
    }

    return count
}

func TestConsume_AMessageRequeuedForEverCarriesOneStampPerCounter(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:            bound.Of(0),
        FailureTransportName:  "failed",
        Backoff:               bound.Backoff{Initial: bound.Of(time.Millisecond)},
        MaxDeadLetterAttempts: bound.UnlimitedCount(),
    })

    source := &recordingNackTransport{}
    envelopeInstance := NewEnvelope(consumeTestMessage{Value: 1}, nil)
    for cycle := 0; cycle < 100; cycle++ {
        mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, envelopeInstance)
        envelopeInstance = source.nackEnvelope
    }

    if 100 != DeadLetterAttemptCount(envelopeInstance) {
        t.Fatalf("expected dead-letter attempt 100, got %d", DeadLetterAttemptCount(envelopeInstance))
    }

    if 1 != countStampsNamed(envelopeInstance, StampNameDeadLetterAttempt) || 1 != countStampsNamed(envelopeInstance, StampNameDelay) {
        t.Fatalf("expected one dead-letter and one delay stamp, got %d and %d", countStampsNamed(envelopeInstance, StampNameDeadLetterAttempt), countStampsNamed(envelopeInstance, StampNameDelay))
    }
}

func TestConsume_AMessageRetriedManyTimesCarriesOneRedeliveryStamp(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{MaxRetries: bound.Of(50), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond)}, MaxDeadLetterAttempts: bound.UnlimitedCount()})

    source := &recordingNackTransport{}
    envelopeInstance := NewEnvelope(consumeTestMessage{Value: 1}, nil)
    for cycle := 0; cycle < 50; cycle++ {
        mustConsumeSession(command, runtimeInstance).consume(runtimeInstance, source, envelopeInstance)
        envelopeInstance = source.nackEnvelope
    }

    if 50 != RedeliveryCount(envelopeInstance) || 1 != countStampsNamed(envelopeInstance, StampNameRedelivery) {
        t.Fatalf("expected one redelivery stamp counting 50, got %d stamps counting %d", countStampsNamed(envelopeInstance, StampNameRedelivery), RedeliveryCount(envelopeInstance))
    }

    if 1 < countStampsNamed(envelopeInstance, StampNameDelay) {
        t.Fatalf("expected at most one delay stamp, got %d", countStampsNamed(envelopeInstance, StampNameDelay))
    }
}

type foreignStampedEnvelope struct {
    stamps []messagebuscontract.Stamp
}

func (instance *foreignStampedEnvelope) Message() any {
    return nil
}

func (instance *foreignStampedEnvelope) Stamps() []messagebuscontract.Stamp {
    return instance.stamps
}

func (instance *foreignStampedEnvelope) WithStamps(stamps []messagebuscontract.Stamp) messagebuscontract.Envelope {
    return &foreignStampedEnvelope{stamps: append(append([]messagebuscontract.Stamp{}, instance.stamps...), stamps...)}
}

func TestWithReplacedStamp_KeepsTheOtherStampsAndAppendsOnAForeignEnvelope(t *testing.T) {
    replaced := withReplacedStamp(NewEnvelope(consumeTestMessage{Value: 1}, []messagebuscontract.Stamp{HandledStamp{HandlerName: "a"}, DelayStamp{Delay: time.Second}}), DelayStamp{Delay: time.Minute})
    if 1 != countStampsNamed(replaced, StampNameHandled) || 1 != countStampsNamed(replaced, StampNameDelay) {
        t.Fatalf("expected the handled stamp kept and one delay stamp, got %v", replaced.Stamps())
    }

    if delayStamp, _ := LastStampOfType[DelayStamp](replaced); time.Minute != delayStamp.Delay {
        t.Fatalf("expected the replacing delay, got %v", delayStamp.Delay)
    }

    foreign := withReplacedStamp(&foreignStampedEnvelope{stamps: []messagebuscontract.Stamp{DelayStamp{Delay: time.Second}}}, DelayStamp{Delay: time.Minute})
    if 2 != countStampsNamed(foreign, StampNameDelay) {
        t.Fatalf("expected the stamp appended on a foreign envelope, got %v", foreign.Stamps())
    }
}

/* the `--limit` and `--concurrency` flags parse in base ten, as the standard integer flags do: with the engine's inferred base --limit=010 would read as eight and --limit=08 would be refused */
func TestConsumeCommand_IntegerFlagsParseInBaseTen(t *testing.T) {
    for _, flagName := range []string{"limit", "concurrency"} {
        for spelling, expected := range map[string]int{"010": 10, "08": 8} {
            observed := -1

            engineCommand := &clicontract.CommandContext{
                Name:  "probe",
                Flags: (&ConsumeCommand{}).Flags(),
                ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
                },
                Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
                    observed = actionCommand.Int(flagName)

                    return nil
                },
            }

            if runErr := engineCommand.Run(context.Background(), []string{"probe", "--" + flagName + "=" + spelling}); nil != runErr {
                t.Fatalf("expected --%s=%s to parse, got %v", flagName, spelling, runErr)
            }

            if expected != observed {
                t.Fatalf("expected --%s=%s to read as %d, got %d", flagName, spelling, expected, observed)
            }
        }
    }
}

/* a stopped worker takes no further delivery: the stop is read before the select, where it and a waiting delivery are both ready and Go picked between them at random */
func TestConsumeFrom_AStoppedWorkerTakesNoFurtherDelivery(t *testing.T) {
    for run := 0; run < 20; run++ {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        cancelledContext, cancel := context.WithCancel(context.Background())
        cancel()
        runtimeInstance := runtime.NewRun(cancelledContext, serviceContainer.NewScope())

        queue := make(chan messagebuscontract.Envelope, 64)
        for value := 0; value < 64; value++ {
            queue <- NewEnvelope(consumeTestMessage{Value: value}, nil)
        }

        locator := NewHandlerLocator()
        var handled int64
        RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
            atomic.AddInt64(&handled, 1)
            return nil
        })

        command := newConsumeCommandOver(NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})}), ConsumeCommandConfig{})

        _ = mustConsumeSession(command, runtimeInstance).consumeFrom(runtimeInstance, &closedQueueTransport{queue: queue}, 0, 1)

        if 0 != atomic.LoadInt64(&handled) {
            t.Fatalf("run %d: expected no delivery taken after the stop, got %d", run, atomic.LoadInt64(&handled))
        }
    }
}

/* a delivery gets a scope of its own; a run whose scope closed has none to open, so the delivery keeps the run's runtime and refuses at its first resolution */
func TestConsume_MessageRuntimeOpensAChildScopeAndKeepsTheRunsWhenItsScopeClosed(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    baseScope := serviceContainer.NewScope()
    runtimeInstance := runtime.NewRun(context.Background(), baseScope)

    bus := NewManager("default", []messagebuscontract.Middleware{})
    session := mustConsumeSession(newConsumeCommandOver(bus, ConsumeCommandConfig{}), runtimeInstance)
    envelope := NewEnvelope(consumeTestMessage{Value: 1}, nil)

    messageRuntime, closeScope := session.messageRuntime(runtimeInstance, envelope)
    if baseScope == messageRuntime.Scope() {
        t.Fatalf("expected the delivery to hold a scope of its own")
    }
    closeScope()

    _ = baseScope.Close()

    closedRuntime, closeClosed := session.messageRuntime(runtimeInstance, envelope)
    defer closeClosed()

    if runtimeInstance != closedRuntime {
        t.Fatalf("expected the run's runtime kept once its scope closed")
    }
}

/* the failure transport is resolved by name from the transports the run reads, so a failure transport the container builds reaches the framework's command; a name no transport carries refuses the run before any message is pulled */
func TestConsume_AFailureTransportNameNoTransportCarriesRefusesTheRun(t *testing.T) {
    command := newConsumeCommandOverTransports(
        NewManager("default", nil),
        ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: "failed"}},
        map[string]messagebuscontract.Transport{"orders": NewInMemoryTransport(InMemoryTransportConfig{})},
    )

    session, sessionErr := command.newConsumeSession(nil)
    if nil != session || nil == sessionErr {
        t.Fatalf("expected the run refused, got session %v and error %v", session, sessionErr)
    }

    refusal, _ := sessionErr.(*exception.Error)
    if nil == refusal || "the retry policy's failure transport names no registered transport" != refusal.Message() || "RetryPolicy.FailureTransportName" != refusal.Context()["field"] || "failed" != refusal.Context()["transport"] {
        t.Fatalf("expected the refusal to name the field and the transport, got %v", sessionErr)
    }
}

func TestConsume_TheFailureTransportIsTheRegisteredTransportItsNameNames(t *testing.T) {
    failure := NewInMemoryTransport(InMemoryTransportConfig{})
    command := newConsumeCommandOverTransports(
        NewManager("default", nil),
        ConsumeCommandConfig{Retry: RetryPolicy{FailureTransportName: "failed"}},
        map[string]messagebuscontract.Transport{"orders": NewInMemoryTransport(InMemoryTransportConfig{}), "failed": failure},
    )

    session, sessionErr := command.newConsumeSession(nil)
    if nil != sessionErr || true != isSameTransport(failure, session.failureTransport) {
        t.Fatalf("expected the session to hold the named transport, got %v %v", session, sessionErr)
    }

    none, noneErr := newConsumeCommandOver(NewManager("default", nil), ConsumeCommandConfig{}).newConsumeSession(nil)
    if nil != noneErr || nil != none.failureTransport {
        t.Fatalf("expected no failure transport without a name, got %v %v", none, noneErr)
    }
}

/* the failure transport is told apart by name: a run reading the transport the policy names discards the exhausted message rather than send it back, whatever the transport's kind */
func TestConsume_ARunReadingTheFailureTransportByNameDoesNotSendItBack(t *testing.T) {
    command, runtimeInstance := newAlwaysFailingConsumeCommand(t, RetryPolicy{
        MaxRetries:            bound.Of(0),
        FailureTransportName:  "failed",
        MaxDeadLetterAttempts: bound.UnlimitedCount(),
        Backoff:               bound.Backoff{Initial: bound.Of(time.Millisecond)},
    })

    session := mustConsumeSession(command, runtimeInstance)
    session.consumedTransportName = "failed"

    source := &recordingNackTransport{}
    session.consume(runtimeInstance, source, NewEnvelope(consumeTestMessage{Value: 1}, nil))
    if 1 != source.nackCount || true == source.nackRequeue {
        t.Fatalf("expected the message nacked without requeue on the failure transport itself, got nackCount=%d requeue=%v", source.nackCount, source.nackRequeue)
    }
}

/* valueTransport is a transport of value kind, which no identity comparison can tell apart, recording what is sent to it */
type valueTransport struct {
    inner *InMemoryTransport
    sent  *atomic.Int64
}

func (instance valueTransport) Send(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    instance.sent.Add(1)

    return instance.inner.Send(runtimeInstance, envelope)
}

func (instance valueTransport) Receive(runtimeInstance containercontract.Runtime) (messagebuscontract.Subscription, error) {
    return instance.inner.Receive(runtimeInstance)
}

func (instance valueTransport) Ack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope) error {
    return instance.inner.Ack(runtimeInstance, envelope)
}

func (instance valueTransport) Nack(runtimeInstance containercontract.Runtime, envelope messagebuscontract.Envelope, config messagebuscontract.NackConfig) error {
    return instance.inner.Nack(runtimeInstance, envelope, config)
}

func (instance valueTransport) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

/* a run of the command reading the transport its policy names as the failure transport discards an exhausted message rather than send it back, even for a transport of value kind */
func TestConsumeCommand_ARunOfTheFailureTransportOfValueKindDoesNotSendItBack(t *testing.T) {
    failure := valueTransport{inner: NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)}), sent: &atomic.Int64{}}

    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message consumeTestMessage, envelope messagebuscontract.Envelope) error {
        return exception.NewError("handler always fails", nil, nil)
    })

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(ServiceBus, func(resolver containercontract.Resolver) (messagebuscontract.Bus, error) {
        return NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})}), nil
    }, container.RegisterOptions{})
    serviceContainer.MustRegister(ServiceTransports, func(resolver containercontract.Resolver) (map[string]messagebuscontract.Transport, error) {
        return map[string]messagebuscontract.Transport{"failed": failure}, nil
    }, container.RegisterOptions{})

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())
    if sendErr := failure.inner.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 7}, nil)); nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    command := NewConsumeCommand(ConsumeCommandConfig{Retry: RetryPolicy{MaxRetries: bound.Of(0), FailureTransportName: "failed"}})

    var runErr error
    engineCommand := &clicontract.CommandContext{
        Name:           command.Name(),
        Flags:          command.Flags(),
        Writer:         io.Discard,
        ErrWriter:      io.Discard,
        ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {},
        Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
            runErr = command.Run(runtimeInstance, actionCommand)

            return runErr
        },
    }
    _ = engineCommand.Run(context.Background(), []string{command.Name(), "--transport=failed", "--limit=1"})

    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if 0 != failure.sent.Load() {
        t.Fatalf("expected nothing sent back to the failure transport, got %d sends", failure.sent.Load())
    }
}
