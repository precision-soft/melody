package messagebus

import (
    "bytes"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/logging"
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

/* the transport closed while the message waited out its delay: the requeue can no longer happen, and the loss is journaled on the logger captured at the Nack */
func TestInMemoryTransport_ADelayedRequeueDroppedAtCloseIsLogged(t *testing.T) {
    transport := NewInMemoryTransport(4)

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    delayed := NewEnvelope(taskCreated{TaskId: 3}).WithStamp(DelayStamp{Delay: time.Hour})
    if nackErr := transport.Nack(runtimeInstance, delayed, true); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    if closeErr := transport.Close(); nil != closeErr {
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

/* emergencyJournalCapture installs, before any goroutine can reach it, an emergency logger writing into a pipe, and
   collects what it writes: the emergency logger reads os.Stderr once, when it is created, so creating it here keeps the
   transport's goroutine from ever reading the variable this test reassigns */
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

/* the logger is resolved at every delayed Nack, and a runtime without one is no loss yet: the emergency line is owed at a drop, not at the Nack */
func TestInMemoryTransport_ADelayedNackOnARuntimeWithoutALoggerWritesNoEmergencyLine(t *testing.T) {
    capture := captureEmergencyJournal(t)

    transport := NewInMemoryTransport(4)

    delayed := NewEnvelope(taskCreated{TaskId: 4}).WithStamp(DelayStamp{Delay: time.Hour})
    if nackErr := transport.Nack(newTestRuntime(), delayed, true); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    /* the capture is closed before it is read: closing waits for its reader to drain the pipe, so a line the Nack wrote
       is in the text rather than still in flight */
    capture.restore()
    written := capture.text()

    _ = transport.Close()

    if true == strings.Contains(written, "could not get the logger from runtime") {
        t.Fatalf("expected no emergency line for a Nack that lost nothing, got %q", written)
    }
}

/* a drop on a Nack whose runtime carried no logger is the moment the line is owed: it goes to the emergency logger */
func TestInMemoryTransport_ADropOnARuntimeWithoutALoggerIsJournaledOnTheEmergencyLogger(t *testing.T) {
    capture := captureEmergencyJournal(t)
    defer capture.restore()

    transport := NewInMemoryTransport(4)

    delayed := NewEnvelope(taskCreated{TaskId: 5}).WithStamp(DelayStamp{Delay: time.Hour})
    if nackErr := transport.Nack(newTestRuntime(), delayed, true); nil != nackErr {
        t.Fatalf("unexpected nack error: %v", nackErr)
    }

    if closeErr := transport.Close(); nil != closeErr {
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
