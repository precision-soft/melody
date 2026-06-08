package messagebus_test

import (
    "sync"
    "testing"
    "time"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/messagebus"
)

type raceTestLogger struct{}

func (raceTestLogger) Log(loggingcontract.Level, string, loggingcontract.Context) {}
func (raceTestLogger) Debug(string, loggingcontract.Context)                       {}
func (raceTestLogger) Info(string, loggingcontract.Context)                        {}
func (raceTestLogger) Warning(string, loggingcontract.Context)                     {}
func (raceTestLogger) Error(string, loggingcontract.Context)                       {}
func (raceTestLogger) Emergency(string, loggingcontract.Context)                   {}

func TestInMemoryTransport_WithLoggerIsRaceFreeWithDelayedRequeue(t *testing.T) {
    transport := messagebus.NewInMemoryTransport(0)
    runtimeInstance := newTestRuntime()

    envelope := messagebus.NewEnvelope(taskCreated{TaskId: 1}, messagebus.DelayStamp{Delay: 100 * time.Microsecond})

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
