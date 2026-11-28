package messagebus

import (
    "bytes"
    "context"
    "errors"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

func TestInMemoryTransport_RequeueOnFullQueueDoesNotBlock(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(1)})
    runtimeInstance := newTestRuntime()

    if sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    nackErr := transport.Nack(runtimeInstance, NewEnvelope(taskCreated{TaskId: 2}, nil), messagebuscontract.NackOptions{Requeue: true})
    if nil == nackErr {
        t.Fatalf("expected nack to report a dropped message when the queue is full")
    }
}

func TestInMemoryTransport_CloseRejectsFurtherSendsAndIsIdempotent(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(1)})
    runtimeInstance := newTestRuntime()

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected second close error: %v", closeErr)
    }

    sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}, nil))
    if nil == sendErr {
        t.Fatalf("expected send to fail after close")
    }
}

func TestNewInMemoryTransport_RefusesANegativeBufferSize(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(-1)})
    }, "the limit is below its minimum")
}

func TestInMemoryTransport_RequeueAfterCloseIsRefusedDeterministically(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(8)})
    runtimeInstance := newTestRuntime()

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    for attempt := 0; attempt < 50; attempt++ {
        if nackErr := transport.Nack(runtimeInstance, NewEnvelope(taskCreated{TaskId: attempt}, nil), messagebuscontract.NackOptions{Requeue: true}); nil == nackErr {
            t.Fatalf("expected every post-close requeue to be refused, attempt %d landed", attempt)
        }
    }
}

func TestInMemoryTransport_DroppedDelayedRequeueIsLoggedThroughTheRuntime(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(1)})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* fill the queue so the deferred requeue has nowhere to land */
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}, nil)); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    delayed := NewEnvelope(taskCreated{TaskId: 2}, nil).WithStamps([]messagebuscontract.Stamp{DelayStamp{Delay: 20 * time.Millisecond}})
    if nackErr := transport.Nack(runtimeInstance, delayed, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if true == logger.hasMessageContaining("dropped a delayed requeue") {
            return
        }
        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("expected the dropped delayed requeue to be logged through the runtime's logger")
}

func TestInMemoryTransport_CloseClosesTheReceiveChannelSoAConsumerSeesEndOfStream(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    queueSubscription, receiveErr := transport.Receive(newTestRuntime())

    queue := queueSubscription.Envelopes()
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    select {
    case _, open := <-queue:
        if true == open {
            t.Fatalf("expected the Receive channel to be closed after Close, got a live delivery")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected Close to close the Receive channel so a consumer ranging it ends; it stayed open")
    }
}

func TestInMemoryTransport_ASubscriptionEndsWithItsRuntimeAndTheQueueServesTheNextOne(t *testing.T) {
    for _, bufferSize := range []int{0, 4} {
        transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(bufferSize)})
        runtimeInstance := newTestRuntime()

        stoppedContext, stop := context.WithCancel(context.Background())
        stoppedSubscription, receiveErr := transport.Receive(runtime.WithContext(runtimeInstance, stoppedContext))
        if nil != receiveErr {
            t.Fatalf("size %d: receive: %v", bufferSize, receiveErr)
        }

        go func() {
            _ = transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 7}, nil))
        }()

        time.Sleep(20 * time.Millisecond)
        stop()

        select {
        case _, open := <-stoppedSubscription.Envelopes():
            for true == open {
                _, open = <-stoppedSubscription.Envelopes()
            }
        case <-time.After(2 * time.Second):
            t.Fatalf("size %d: expected the stream to close once its runtime ended, with the transport still open", bufferSize)
        }

        if nil != stoppedSubscription.Err() {
            t.Fatalf("size %d: expected no reason for a stop asked through the runtime, got %v", bufferSize, stoppedSubscription.Err())
        }

        nextSubscription, _ := transport.Receive(runtimeInstance)

        select {
        case envelopeInstance, open := <-nextSubscription.Envelopes():
            if false == open {
                t.Fatalf("size %d: expected the shared queue to stay open for the next subscription", bufferSize)
            }

            if 7 != envelopeInstance.Message().(taskCreated).TaskId {
                t.Fatalf("size %d: expected the delivery the stopped subscription did not hand on, got %v", bufferSize, envelopeInstance.Message())
            }
        case <-time.After(2 * time.Second):
            t.Fatalf("size %d: expected the next subscription to receive the delivery", bufferSize)
        }

        _ = transport.CloseWithContext(t.Context())
    }
}

func TestInMemoryTransport_AnIdleSubscriptionClosesItsStreamWhenItsRuntimeEnds(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{})
    defer func() {
        _ = transport.CloseWithContext(t.Context())
    }()

    stoppedContext, stop := context.WithCancel(context.Background())
    subscription, _ := transport.Receive(runtime.WithContext(newTestRuntime(), stoppedContext))

    stop()

    select {
    case _, open := <-subscription.Envelopes():
        if true == open {
            t.Fatalf("expected no delivery on an idle subscription")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the idle subscription's stream to close once its runtime ended, with the transport still open")
    }
}

func TestInMemoryTransport_ReceiveRefusesANilRuntime(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{})

    subscription, receiveErr := transport.Receive(nil)
    if nil == receiveErr || nil != subscription {
        t.Fatalf("expected a nil runtime refused with no subscription, got %v and %v", subscription, receiveErr)
    }
}

func TestInMemoryTransport_ConcurrentSendsAndCloseAreRaceFreeAndNeverPanic(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(0)})
    runtimeInstance := newTestRuntime()

    var senders sync.WaitGroup
    for sender := 0; sender < 8; sender++ {
        senders.Add(1)
        go func() {
            defer senders.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _ = transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: iteration}, nil))
            }
        }()
    }

    /* a reader drains so unbuffered sends can make progress until Close lands */
    stopReader := make(chan struct{})
    queueSubscription, _ := transport.Receive(runtimeInstance)
    queue := queueSubscription.Envelopes()
    go func() {
        for {
            select {
            case <-stopReader:
                return
            case _, open := <-queue:
                if false == open {
                    return
                }
            }
        }
    }()

    time.Sleep(2 * time.Millisecond)
    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    senders.Wait()
    close(stopReader)
}

func TestInMemoryTransport_ADelayedRequeueDroppedAtCloseIsLogged(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    delayed := NewEnvelope(taskCreated{TaskId: 3}, nil).WithStamps([]messagebuscontract.Stamp{DelayStamp{Delay: time.Hour}})
    if nackErr := transport.Nack(runtimeInstance, delayed, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if true == logger.hasMessageContaining("the transport was closed before its delay ran out") {
            return
        }
        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("expected the requeue dropped at close to be logged through the runtime's logger")
}

type emergencyJournalCapture struct {
    mutex    sync.Mutex
    written  bytes.Buffer
    finished chan struct{}
    restore  func()
}

func captureEmergencyJournal(t *testing.T) *emergencyJournalCapture {
    t.Helper()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    capture := &emergencyJournalCapture{finished: make(chan struct{})}

    savedStandardError := os.Stderr
    logging.CloseEmergencyLogger()
    os.Stderr = writeEnd
    _ = logging.EmergencyLogger()
    os.Stderr = savedStandardError

    go func() {
        defer close(capture.finished)

        chunk := make([]byte, 4096)
        for {
            read, readErr := readEnd.Read(chunk)
            capture.mutex.Lock()
            capture.written.Write(chunk[:read])
            capture.mutex.Unlock()

            if nil != readErr {
                return
            }
        }
    }()

    capture.restore = func() {
        logging.CloseEmergencyLogger()
        _ = writeEnd.Close()
        <-capture.finished
        _ = readEnd.Close()
    }

    return capture
}

func (instance *emergencyJournalCapture) text() string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.written.String()
}

func TestInMemoryTransport_ADelayedNackOnARuntimeWithoutALoggerWritesNoEmergencyLine(t *testing.T) {
    capture := captureEmergencyJournal(t)

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    delayed := NewEnvelope(taskCreated{TaskId: 4}, nil).WithStamps([]messagebuscontract.Stamp{DelayStamp{Delay: time.Hour}})
    if nackErr := transport.Nack(newTestRuntime(), delayed, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    /* closed before it is read: closing waits for its reader to drain the pipe */
    capture.restore()
    written := capture.text()

    _ = transport.CloseWithContext(t.Context())

    if true == strings.Contains(written, "could not get the logger from runtime") {
        t.Fatalf("expected no emergency line for a Nack that lost nothing, got %q", written)
    }
}

func TestInMemoryTransport_ADropOnARuntimeWithoutALoggerIsJournaledOnTheEmergencyLogger(t *testing.T) {
    capture := captureEmergencyJournal(t)
    defer capture.restore()

    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    delayed := NewEnvelope(taskCreated{TaskId: 5}, nil).WithStamps([]messagebuscontract.Stamp{DelayStamp{Delay: time.Hour}})
    if nackErr := transport.Nack(newTestRuntime(), delayed, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    if closeErr := transport.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if true == strings.Contains(capture.text(), "the transport was closed before its delay ran out") {
            return
        }
        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("expected the drop journaled on the emergency logger, got %q", capture.text())
}

func TestInMemoryTransport_ARequeueOnAnUnbufferedQueueReachesTheNextReceive(t *testing.T) {
    for _, bufferSize := range []int{0, 8} {
        transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(bufferSize)})
        runtimeInstance := newTestRuntime()

        receivedSubscription, _ := transport.Receive(runtimeInstance)

        received := receivedSubscription.Envelopes()

        go func() {
            _ = transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}, nil))
        }()

        attempts := 0
        for attempts < 3 {
            select {
            case envelopeInstance := <-received:
                attempts++
                if 3 > attempts {
                    if nackErr := transport.Nack(runtimeInstance, envelopeInstance, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
                        t.Fatalf("size %d: unexpected nack error on attempt %d: %v", bufferSize, attempts, nackErr)
                    }
                }
            case <-time.After(2 * time.Second):
                t.Fatalf("size %d: expected the requeued message received again, got %d attempts", bufferSize, attempts)
            }
        }

        _ = transport.CloseWithContext(t.Context())
    }
}

func TestInMemoryTransport_ARequeueWaitingOnAnUnbufferedQueueIsDroppedAtCloseAndLogged(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(0)})
    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    if nackErr := transport.Nack(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}, nil), messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    _ = transport.CloseWithContext(t.Context())

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if true == logger.hasMessageContaining("dropped a delayed requeue") {
            return
        }
        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("expected the requeue waiting at close to be logged as dropped")
}

func TestNewInMemoryTransport_AnAbsentBufferSizeIsSixtyFour(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{})
    if 64 != cap(transport.queue) {
        t.Fatalf("expected a 64-slot queue, got %d", cap(transport.queue))
    }

    unbuffered := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(0)})
    if 0 != cap(unbuffered.queue) {
        t.Fatalf("expected an unbuffered queue, got %d", cap(unbuffered.queue))
    }
}

func TestNewInMemoryTransport_RefusesAnUnlimitedBufferSizeAndATypedNilLogger(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.UnlimitedCount()})
    }, "the limit does not admit unlimited")

    var typedNilLogger *messageRecordingLogger
    testhelper.AssertPanicsWithError(t, func() {
        NewInMemoryTransport(InMemoryTransportConfig{Logger: typedNilLogger})
    }, "in-memory transport logger is a typed nil")
}

func TestNewInMemoryTransport_AnAbsentLoggerIsTheEmergencyLogger(t *testing.T) {
    emergency, _ := logging.ResolveOptionalLogger(nil, "in-memory transport", "InMemoryTransportConfig.Logger")

    if emergency != NewInMemoryTransport(InMemoryTransportConfig{}).resolveLogger(newTestRuntime()) {
        t.Fatalf("expected an absent logger to be the emergency logger where the runtime carries none")
    }
}

func TestInMemoryTransport_TheConfiguredLoggerJournalsADropWhenTheRuntimeHasNone(t *testing.T) {
    logger := &messageRecordingLogger{}
    transport := NewInMemoryTransport(InMemoryTransportConfig{Logger: logger})

    if logger != transport.resolveLogger(newTestRuntime()) {
        t.Fatalf("expected the configured logger where the runtime carries none")
    }
}

func TestInMemoryTransport_RefusesHeadersAboveItsLimit(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4), Headers: HeaderLimitsConfig{MaxCount: bound.Of(1)}})
    defer transport.CloseWithContext(context.Background())

    refused := NewEnvelope("payload", []messagebuscontract.Stamp{HeadersStamp{Headers: map[string]string{"a": "1", "b": "2"}}})
    sendErr := transport.Send(newTestRuntime(), refused)
    if false == errors.Is(sendErr, ErrHeadersRefused) {
        t.Fatalf("expected the send refused by ErrHeadersRefused, got %v", sendErr)
    }

    if "InMemoryTransportConfig.Headers.MaxCount" != exception.LogContext(sendErr, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected the refusal to name the in-memory field, got %v", exception.LogContext(sendErr, exception.LogContextOptions{}))
    }

    if 0 != len(transport.queue) {
        t.Fatalf("expected nothing queued for a refused send, got %d", len(transport.queue))
    }

    admitted := NewEnvelope("payload", []messagebuscontract.Stamp{HeadersStamp{Headers: map[string]string{"a": "1"}}})
    if sendErr := transport.Send(newTestRuntime(), admitted); nil != sendErr {
        t.Fatalf("expected one header admitted, got %v", sendErr)
    }

    received := <-transport.queue
    if "1" != Headers(received)["a"] {
        t.Fatalf("expected the header to cross, got %v", Headers(received))
    }
}

func TestNewInMemoryTransport_RefusesUnlimitedHeaderLimitsNamingTheField(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "InMemoryTransportConfig.Headers.MaxBytes" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected a refusal naming InMemoryTransportConfig.Headers.MaxBytes, got %v", recoveredErr)
        }
    }()

    NewInMemoryTransport(InMemoryTransportConfig{Headers: HeaderLimitsConfig{MaxBytes: bound.UnlimitedCount()}})
}
