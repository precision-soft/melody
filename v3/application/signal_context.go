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

/* signalContextForceExitDebounce is how long after the first signal a second one is still treated as a duplicate delivery of the same logical shutdown request — a supervisor and a terminal each forwarding one interrupt land within milliseconds of each other — and absorbed, rather than read as an operator's escalation; tests replace it to drive both paths without real waits */
var signalContextForceExitDebounce = 500 * time.Millisecond

/*
NewSignalContext returns a context that is cancelled by the first SIGINT or SIGTERM, giving the application a graceful shutdown window. A second SIGINT or SIGTERM received while that shutdown is still running prints one line to stderr and forces the process to exit with the conventional 128+signal code, so an operator facing a hung shutdown is never reduced to SIGKILL; a second signal landing within half a second of the first is absorbed as a duplicate delivery of the same shutdown request, so a supervisor and a terminal both forwarding one interrupt do not skip the graceful shutdown. The forced exit deliberately runs no teardown: it exists for the shutdown that is already hung, and a teardown on its path could hang the same way — the escalation is the operator's demand for a process that is gone now, at the acknowledged price of whatever a Close would have flushed.

The returned stop function unregisters the signal notifications, cancels the context, and releases the watcher goroutine; it is safe to call more than once and from concurrent goroutines. Calling it is not optional in a process that outlives the application: a watcher left armed stays registered, so the next application cycle in the same process runs under two watchers, and the older one — already past its first signal — reads the next SIGTERM as the escalation and forces exit 143 where a graceful shutdown was owed.
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
            /* the stop channel closes before the notifications unregister: the watcher's guards key on it, so any signal still buffered from before the unregistration is provably stale by the time the watcher could act on it */
            close(stopChannel)
            signal.Stop(signalChannel)
            <-doneChannel
            cancel()
        })
    }

    return signalContext, stop
}

/* watchSignals cancels the context on the first received signal and forces the process down on a second one, unless that second delivery lands inside the debounce window of the first — a near-simultaneous duplicate of the same shutdown request — in which case it is absorbed and the watcher keeps waiting; a requested stop always wins over a signal that is merely buffered, so a caller that has unregistered can never be taken down by a stale delivery */
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
