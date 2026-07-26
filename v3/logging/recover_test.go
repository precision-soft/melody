package logging

import (
    "io"
    "os"
    "os/exec"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* the marker tells a re-executed test binary that it is the child that has to survive the recovered exit error rather than the parent that watches it */
const logOnRecoverExitProbeMarker = "MELODY_LOG_ON_RECOVER_EXIT_PROBE"

func TestLogOnRecover_DoesNothingWhenNoPanic(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected no log calls")
    }
}

func TestLogOnRecover_LogsExceptionError(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        exception.Panic(exception.NewError("boom", map[string]any{"a": "b"}, nil))
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one log call")
    }

    if loggingcontract.LevelError != logger.lastLevel {
        t.Fatalf("unexpected level")
    }

    if "boom" != logger.lastMessage {
        t.Fatalf("unexpected message")
    }

    if "b" != logger.lastContext["a"] {
        t.Fatalf("unexpected context")
    }
}

func TestLogOnRecover_SkipsAlreadyLoggedException(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        err := exception.NewError("boom", nil, nil)
        err.MarkAsLogged()

        exception.Panic(err)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected no log calls")
    }
}

func TestLogOnRecover_PanicAgainRePanicsAndMarksLogged(t *testing.T) {
    logger := &captureLogger{}

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }

        err, ok := recoveredValue.(*exception.Error)
        if false == ok {
            t.Fatalf("expected *exception.Error")
        }

        if false == err.AlreadyLogged() {
            t.Fatalf("expected error to be marked as logged")
        }
    }()

    func() {
        defer LogOnRecover(logger, true)

        exception.Panic(exception.NewError("boom", nil, nil))
    }()
}

/* @info the subprocess is the assertion: LogOnRecover runs on top of every defer registered before it, so an os.Exit inside it cannot be observed from the same goroutine — the process is simply gone, together with the container teardown and the shutdown hooks those defers hold. Re-running this test in a child with the marker set lets the parent read the child's exit status and its output, which is the only place the difference between "logged and returned" and "terminated the process" is visible. */
func TestLogOnRecover_DoesNotTerminateTheProcessOnAnExitError(t *testing.T) {
    if "1" == os.Getenv(logOnRecoverExitProbeMarker) {
        logger := &captureLogger{}

        func() {
            defer func() {
                _, _ = os.Stdout.WriteString("deferred-below-ran\n")
            }()

            defer LogOnRecover(logger, false)

            exception.Exit(
                exception.NewExitError(9, exception.NewError("boom", nil, nil)),
            )
        }()

        if 1 != logger.calls {
            _, _ = os.Stdout.WriteString("expected-one-log-call\n")

            return
        }

        _, _ = os.Stdout.WriteString("survived\n")

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestLogOnRecover_DoesNotTerminateTheProcessOnAnExitError$",
    )
    command.Env = append(os.Environ(), logOnRecoverExitProbeMarker+"=1")

    output, runErr := command.CombinedOutput()
    if nil != runErr {
        t.Fatalf("expected the child to finish normally, got %v with output %q", runErr, string(output))
    }

    if false == strings.Contains(string(output), "survived") {
        t.Fatalf("expected LogOnRecover to log and return, got %q", string(output))
    }

    if false == strings.Contains(string(output), "deferred-below-ran") {
        t.Fatalf("expected the defers registered below LogOnRecover to still run, got %q", string(output))
    }
}

/* @info the exit code is the whole point of an *exception.ExitError, so the re-panic must carry the wrapper and not the error inside it: an outer handler reads the code off the wrapper, and unwrapping here would quietly downgrade a deliberate code to whatever that handler falls back to */
func TestLogOnRecover_PanicAgainCarriesTheExitErrorOnward(t *testing.T) {
    logger := &captureLogger{}

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the exit error to travel on")
        }

        exitError, isExitError := recoveredValue.(*exception.ExitError)
        if false == isExitError {
            t.Fatalf("expected *exception.ExitError, got %T", recoveredValue)
        }

        if 9 != exitError.ExitCode() {
            t.Fatalf("expected the exit code to survive, got %d", exitError.ExitCode())
        }

        if 1 != logger.calls {
            t.Fatalf("expected the exit error to be logged once, got %d", logger.calls)
        }
    }()

    func() {
        defer LogOnRecover(logger, true)

        exception.Exit(
            exception.NewExitError(9, exception.NewError("boom", nil, nil)),
        )
    }()
}

func TestEchoExitToStderr_WritesOneLineForNonZeroExit(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    echoExitToStderr(exception.NewError("http server error", nil, nil), 1)

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    /* @important a fatal non-zero exit must leave a visible trace on stderr even when the configured logger writes elsewhere (e.g. a file logger), which would otherwise exit completely silently on the AlreadyLogged path */
    if false == strings.Contains(string(output), "melody: exiting with code 1") || false == strings.Contains(string(output), "http server error") {
        t.Fatalf("expected the exit echo line on stderr, got %q", string(output))
    }
}

func TestEchoExitToStderr_StaysSilentForZeroExit(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    echoExitToStderr(exception.NewError("clean exit", nil, nil), 0)

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if "" != string(output) {
        t.Fatalf("expected no stderr echo for a zero exit code, got %q", string(output))
    }
}
