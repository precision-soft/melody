package messagebus

import (
    "sync"
    "testing"
    "time"

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

    if closeErr := transport.Close(runtimeInstance); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if closeErr := transport.Close(runtimeInstance); nil != closeErr {
        t.Fatalf("unexpected second close error: %v", closeErr)
    }

    sendErr := transport.Send(runtimeInstance, NewEnvelope(taskCreated{TaskId: 1}))
    if nil == sendErr {
        t.Fatalf("expected send to fail after close")
    }
}

/* @info logger race */

type raceTestLogger struct{}

func (raceTestLogger) Log(loggingcontract.Level, string, loggingcontract.Context) {}
func (raceTestLogger) Debug(string, loggingcontract.Context)                       {}
func (raceTestLogger) Info(string, loggingcontract.Context)                        {}
func (raceTestLogger) Warning(string, loggingcontract.Context)                     {}
func (raceTestLogger) Error(string, loggingcontract.Context)                       {}
func (raceTestLogger) Emergency(string, loggingcontract.Context)                   {}

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
