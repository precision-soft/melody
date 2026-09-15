package application

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"
)

var signalContextExit = os.Exit

var signalContextForceExitDebounce = 500 * time.Millisecond

/* NewSignalContext cancels on the first SIGINT or SIGTERM. A second signal more than half a second later prints a diagnostic and exits with 128+signal without teardown; closer duplicate signals are absorbed. The returned stop function unregisters signals, cancels the context and releases the watcher. It is concurrency-safe and must be called by hosts that outlive the application. */
func NewSignalContext() (context.Context, context.CancelFunc) {
    signalChannel := make(chan os.Signal, 2)
    signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

    signalContext, cancel := context.WithCancel(context.Background())

    stopChannel := make(chan struct{})
    doneChannel := make(chan struct{})

    go watchSignals(signalChannel, cancel, stopChannel, doneChannel)

    var stopOnce sync.Once
    stop := func() {
        stopOnce.Do(func() {

            close(stopChannel)
            signal.Stop(signalChannel)
            <-doneChannel
            cancel()
        })
    }

    return signalContext, stop
}

func watchSignals(
    signalChannel <-chan os.Signal,
    cancel context.CancelFunc,
    stopChannel <-chan struct{},
    doneChannel chan<- struct{},
) {
    defer close(doneChannel)

    var firstSignalAt time.Time

    select {
    case <-stopChannel:
        return

    case <-signalChannel:
        select {
        case <-stopChannel:
            return

        default:
        }

        cancel()
        firstSignalAt = time.Now()
    }

    for {
        select {
        case <-stopChannel:
            return

        case secondSignal := <-signalChannel:
            select {
            case <-stopChannel:
                return

            default:
            }

            if time.Since(firstSignalAt) < signalContextForceExitDebounce {
                continue
            }

            exitCode := 130
            if signalNumber, ok := secondSignal.(syscall.Signal); true == ok {
                exitCode = 128 + int(signalNumber)
            }

            _, _ = fmt.Fprintf(os.Stderr, "melody: received a second interrupt signal (%s) during shutdown, forcing exit with code %d\n", secondSignal, exitCode)

            signalContextExit(exitCode)

            return
        }
    }
}
