package application

import (
    "context"
    "os"
    "sync"
    "syscall"
    "testing"
    "time"
)

/* signal.NotifyContext keeps SIGINT/SIGTERM registered after the first signal cancels the context, so a second Ctrl+C during a hung shutdown does nothing and the operator's only escape is SIGKILL. The watcher must cancel on the first signal and force the process to exit on the second, with the conventional 128+signal exit code. */
func TestWatchSignals_FirstSignalCancels_SecondForcesExit(t *testing.T) {
    cases := []struct {
        name             string
        secondSignal     os.Signal
        expectedExitCode int
    }{
        {name: "second SIGINT exits 130", secondSignal: syscall.SIGINT, expectedExitCode: 130},
        {name: "second SIGTERM exits 143", secondSignal: syscall.SIGTERM, expectedExitCode: 143},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            exitCalls := make(chan int, 1)
            originalExit := signalContextExit
            signalContextExit = func(code int) {
                exitCalls <- code
            }
            originalDebounce := signalContextForceExitDebounce
            signalContextForceExitDebounce = 0
            defer func() {
                signalContextExit = originalExit
                signalContextForceExitDebounce = originalDebounce
            }()

            signalChannel := make(chan os.Signal, 2)
            stopChannel := make(chan struct{})
            doneChannel := make(chan struct{})

            signalContext, cancel := context.WithCancel(context.Background())
            defer cancel()

            go watchSignals(signalChannel, cancel, stopChannel, doneChannel)
            defer close(stopChannel)

            signalChannel <- syscall.SIGINT

            select {
            case <-signalContext.Done():
            case <-time.After(2 * time.Second):
                t.Fatalf("expected the first signal to cancel the context")
            }

            select {
            case code := <-exitCalls:
                t.Fatalf("expected no forced exit after a single signal, got exit code %d", code)
            default:
            }

            signalChannel <- testCase.secondSignal

            select {
            case code := <-exitCalls:
                if testCase.expectedExitCode != code {
                    t.Fatalf("expected forced exit code %d, got %d", testCase.expectedExitCode, code)
                }
            case <-time.After(2 * time.Second):
                t.Fatalf("expected the second signal to force an exit")
            }

            select {
            case <-doneChannel:
            case <-time.After(2 * time.Second):
                t.Fatalf("expected the watcher goroutine to finish after the forced exit")
            }
        })
    }
}

/* The stop function keeps the signal.NotifyContext contract: it unregisters the notifications, cancels the context, releases the watcher goroutine, and stays safe to call more than once and from concurrent goroutines. */
func TestNewSignalContext_StopCancelsAndReleasesWatcher(t *testing.T) {
    signalContext, stop := NewSignalContext()

    select {
    case <-signalContext.Done():
        t.Fatalf("expected the context to be live before stop")
    default:
    }

    stopReturned := make(chan struct{})
    go func() {
        var waitGroup sync.WaitGroup
        for callerIndex := 0; callerIndex < 3; callerIndex++ {
            waitGroup.Add(1)
            go func() {
                defer waitGroup.Done()
                stop()
            }()
        }
        waitGroup.Wait()
        close(stopReturned)
    }()

    select {
    case <-stopReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected stop to return promptly and release the watcher goroutine")
    }

    select {
    case <-signalContext.Done():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected stop to cancel the context")
    }
}

/* a second signal landing inside the debounce window is a duplicate delivery of the same logical shutdown request — a supervisor and a terminal both forwarding one interrupt — and must be absorbed so the graceful shutdown continues; a signal past the window is the operator's escalation and still forces the exit. */
func TestWatchSignals_NearSimultaneousDuplicateIsAbsorbed(t *testing.T) {
    exitCalls := make(chan int, 1)
    originalExit := signalContextExit
    signalContextExit = func(code int) {
        exitCalls <- code
    }
    originalDebounce := signalContextForceExitDebounce
    signalContextForceExitDebounce = 200 * time.Millisecond
    defer func() {
        signalContextExit = originalExit
        signalContextForceExitDebounce = originalDebounce
    }()

    signalChannel := make(chan os.Signal, 2)
    stopChannel := make(chan struct{})
    doneChannel := make(chan struct{})

    signalContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    /* both deliveries of the one logical interrupt sit buffered before the watcher runs, the worst case for the duplicate window */
    signalChannel <- syscall.SIGTERM
    signalChannel <- syscall.SIGTERM

    go watchSignals(signalChannel, cancel, stopChannel, doneChannel)
    defer close(stopChannel)

    select {
    case <-signalContext.Done():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the first signal to cancel the context")
    }

    select {
    case code := <-exitCalls:
        t.Fatalf("expected the near-simultaneous duplicate to be absorbed, got exit code %d", code)
    case <-time.After(300 * time.Millisecond):
    }

    signalChannel <- syscall.SIGTERM

    select {
    case code := <-exitCalls:
        if 143 != code {
            t.Fatalf("expected forced exit code 143, got %d", code)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected a signal past the debounce window to force the exit")
    }

    select {
    case <-doneChannel:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the watcher goroutine to finish after the forced exit")
    }
}

/* A stop that races a buffered second signal must side with stop: once the caller has unregistered, the watcher may not force the process down. */
func TestWatchSignals_StopWinsOverBufferedSecondSignal(t *testing.T) {
    exitCalls := make(chan int, 1)
    originalExit := signalContextExit
    signalContextExit = func(code int) {
        exitCalls <- code
    }
    defer func() {
        signalContextExit = originalExit
    }()

    signalChannel := make(chan os.Signal, 2)
    stopChannel := make(chan struct{})
    doneChannel := make(chan struct{})

    _, cancel := context.WithCancel(context.Background())
    defer cancel()

    signalChannel <- syscall.SIGINT
    signalChannel <- syscall.SIGINT
    close(stopChannel)

    go watchSignals(signalChannel, cancel, stopChannel, doneChannel)

    select {
    case <-doneChannel:
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the watcher goroutine to finish once stop is requested")
    }

    select {
    case code := <-exitCalls:
        t.Fatalf("expected no forced exit once stop was requested, got exit code %d", code)
    default:
    }
}
