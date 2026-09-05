package messagebus

import (
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func TestInMemoryTransport_RequeueOnFullQueueDoesNotBlock(t *testing.T) {
    transport := NewInMemoryTransport(1)
    runtimeInstance := newTestRuntime()

    if sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    nackErr := transport.Nack(runtimeInstance, NewEnvelope(taskCreated{TaskId: 2}), true)
    if nil == nackErr {
        t.Fatalf("expected nack to report a dropped message when the queue is full")
    }
}

func TestInMemoryTransport_CloseRejectsFurtherSendsAndIsIdempotent(t *testing.T) {
    transport := NewInMemoryTransport(1)
    runtimeInstance := newTestRuntime()

    if closeErr := transport.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := transport.Close(); nil != closeErr {
        t.Fatalf("unexpected second close error: %v", closeErr)
    }

    sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}))
    if nil == sendErr {
        t.Fatalf("expected send to fail after close")
    }
}

type raceTestLogger struct{}

func (instance raceTestLogger) Log(loggingcontract.Level, string, loggingcontract.Context) {}
func (instance raceTestLogger) Debug(string, loggingcontract.Context)                      {}
func (instance raceTestLogger) Info(string, loggingcontract.Context)                       {}
func (instance raceTestLogger) Warning(string, loggingcontract.Context)                    {}
func (instance raceTestLogger) Error(string, loggingcontract.Context)                      {}
func (instance raceTestLogger) Emergency(string, loggingcontract.Context)                  {}

func TestInMemoryTransport_WithLoggerIsRaceFreeWithDelayedRequeue(t *testing.T) {
    transport := NewInMemoryTransport(0)
    runtimeInstance := newTestRuntime()

    envelope := NewEnvelope(taskCreated{TaskId: 1}, DelayStamp{Delay: 100 * time.Microsecond})

    stop := make(chan struct{})
    var writers sync.WaitGroup
    writers.Add(1)
    go func() {
        defer writers.Done()
        for {
            select {
            case <-stop:
                return
            default:
                transport.WithLogger(raceTestLogger{})
            }
        }
    }()

    for iteration := 0; iteration < 300; iteration++ {
        if nackErr := transport.Nack(runtimeInstance, envelope, true); nil != nackErr {
            t.Fatalf("unexpected nack error: %v", nackErr)
        }
    }

    time.Sleep(30 * time.Millisecond)
    close(stop)
    writers.Wait()
}

func TestNewInMemoryTransport_RefusesANegativeBufferSize(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewInMemoryTransport(-1)
    }, "in-memory transport buffer size may not be negative")
}

func TestInMemoryTransport_RequeueAfterCloseIsRefusedDeterministically(t *testing.T) {
    transport := NewInMemoryTransport(8)
    runtimeInstance := newTestRuntime()

    if closeErr := transport.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    /* before the guard, one select weighed a ready queue slot against the closed transport and Go picks between ready cases at RANDOM — a post-Close requeue succeeded about half the time, so "closed" was enforced on Send and coin-flipped on Nack; fifty attempts make a surviving coin flip astronomically unlikely */
    for attempt := 0; attempt < 50; attempt++ {
        if nackErr := transport.Nack(runtimeInstance, NewEnvelope(taskCreated{TaskId: attempt}), true); nil == nackErr {
            t.Fatalf("expected every post-close requeue to be refused, attempt %d landed", attempt)
        }
    }
}

func TestInMemoryTransport_DroppedDelayedRequeueIsLoggedThroughTheRuntime(t *testing.T) {
    transport := NewInMemoryTransport(1)

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* fill the queue so the deferred requeue has nowhere to land */
    if sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1})); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    delayed := NewEnvelope(taskCreated{TaskId: 2}).WithStamp(DelayStamp{Delay: 20 * time.Millisecond})
    if nackErr := transport.Nack(runtimeInstance, delayed, true); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    /* the Nack already answered success and the drop happens later on a detached goroutine: the logger captured from the Nack's runtime is the only witness — the transport's own WithLogger is wired by nothing in any production assembly */
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
    transport := NewInMemoryTransport(4)

    queue, receiveErr := transport.Receive(newTestRuntime())
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    if closeErr := transport.Close(); nil != closeErr {
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

func TestInMemoryTransport_ConcurrentSendsAndCloseAreRaceFreeAndNeverPanic(t *testing.T) {
    transport := NewInMemoryTransport(0)
    runtimeInstance := newTestRuntime()

    var senders sync.WaitGroup
    for sender := 0; sender < 8; sender++ {
        senders.Add(1)
        go func() {
            defer senders.Done()
            for iteration := 0; iteration < 200; iteration++ {
                /* a panic here — a send onto a closed queue — fails the test rather than crashing the binary */
                _ = transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: iteration}))
            }
        }()
    }

    /* a reader drains so unbuffered sends can make progress until Close lands */
    stopReader := make(chan struct{})
    queue, _ := transport.Receive(runtimeInstance)
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
    if closeErr := transport.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    senders.Wait()
    close(stopReader)
}
