package application

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "sync"
    "syscall"
)

/* signalContextExit terminates the process when a second interrupt signal arrives while the graceful shutdown triggered by the first is still running; tests replace it to observe the exit code without stopping the test binary */
var signalContextExit = os.Exit

/*
NewSignalContext returns a context that is cancelled by the first SIGINT or SIGTERM, giving the application a graceful shutdown window. A second SIGINT or SIGTERM received while that shutdown is still running prints one line to stderr and forces the process to exit with the conventional 128+signal code, so an operator facing a hung shutdown is never reduced to SIGKILL.

The returned stop function unregisters the signal notifications, cancels the context, and releases the watcher goroutine; it is safe to call more than once and from concurrent goroutines.
*/
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
            signal.Stop(signalChannel)
            close(stopChannel)
            <-doneChannel
            cancel()
        })
    }

    return signalContext, stop
}

/* watchSignals cancels the context on the first received signal and forces the process down on the second; a requested stop always wins over a signal that is merely buffered, so a caller that has unregistered can never be taken down by a stale delivery */
func watchSignals(
    signalChannel <-chan os.Signal,
    cancel context.CancelFunc,
    stopChannel <-chan struct{},
    doneChannel chan<- struct{},
) {
    defer close(doneChannel)

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
    }

    select {
    case <-stopChannel:
        return

    case secondSignal := <-signalChannel:
        select {
        case <-stopChannel:
            return

        default:
        }

        exitCode := 130
        if signalNumber, ok := secondSignal.(syscall.Signal); true == ok {
            exitCode = 128 + int(signalNumber)
        }

        _, _ = fmt.Fprintf(os.Stderr, "melody: received a second interrupt signal (%s) during shutdown, forcing exit with code %d\n", secondSignal, exitCode)

        signalContextExit(exitCode)
    }
}
