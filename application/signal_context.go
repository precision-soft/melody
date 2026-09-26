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

/* signalContextExit terminates the process when a second interrupt signal arrives while the graceful shutdown triggered by the first is still running; tests replace it to observe the exit code without stopping the test binary */
var signalContextExit = os.Exit

/* signalContextForceExitDebounce is how long after the first signal a second one is absorbed as a duplicate delivery of the same shutdown, a supervisor and a terminal each forwarding one, rather than read as an escalation; tests replace it */
var signalContextForceExitDebounce = 500 * time.Millisecond

/* NewSignalContext returns a context cancelled by the first SIGINT or SIGTERM. A second one while that shutdown runs prints one line to stderr and exits with 128+signal, running no teardown, since the shutdown it escalates is the one that hung; a second signal within half a second of the first is absorbed as a duplicate delivery. The returned stop function unregisters the notifications, cancels the context and releases the watcher; it is safe to call more than once and concurrently. */
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
            /* the stop channel closes before the notifications unregister, so a signal still buffered from before is stale by the time the watcher could act on it */
            close(stopChannel)
            signal.Stop(signalChannel)
            <-doneChannel
            cancel()
        })
    }

    return signalContext, stop
}

/* watchSignals cancels the context on the first signal and forces the process down on a second one outside the debounce window; a requested stop always wins over a buffered signal */
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
