package logging

import (
    "io"
    "os"
    "os/exec"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* @info resolveRecoveredExit is the pure half of LogOnRecoverAndExit: everything below proves the normalization without taking the process exit. */

func TestResolveRecoveredExit_ZeroValueExitErrorDoesNotPanicTheExitHandler(t *testing.T) {
    err, exitCode, needsLogging := resolveRecoveredExit(&exception.ExitError{}, 1)

    if nil == err {
        t.Fatalf("expected a substitute error for an exit error carrying no error value")
    }

    if false == strings.Contains(err.Error(), "exit requested with no error value") {
        t.Fatalf("expected the substitute error to name the anomaly, got %q", err.Error())
    }

    if 0 != exitCode {
        t.Fatalf("expected the zero value's own exit code to be honored, got %d", exitCode)
    }

    if false == needsLogging {
        t.Fatalf("expected the anomaly to be logged")
    }
}

func TestResolveRecoveredExit_ExitErrorKeepsItsCodeAndLogsOnce(t *testing.T) {
    carried := exception.NewError("carried", nil, nil)

    err, exitCode, needsLogging := resolveRecoveredExit(exception.NewExitError(7, carried), 1)

    if carried != err {
        t.Fatalf("expected the carried error back")
    }

    if 7 != exitCode {
        t.Fatalf("expected the exit error's own code, got %d", exitCode)
    }

    if false == needsLogging {
        t.Fatalf("expected an unlogged exit error to need logging")
    }

    carried.MarkAsLogged()

    _, _, needsLoggingAgain := resolveRecoveredExit(exception.NewExitError(7, carried), 1)
    if true == needsLoggingAgain {
        t.Fatalf("expected an already-logged exit error not to be logged again")
    }
}

func TestResolveRecoveredExit_AlreadyLoggedErrorIsNotLoggedAgain(t *testing.T) {
    loggedErr := exception.NewError("already reported", nil, nil)
    loggedErr.MarkAsLogged()

    err, exitCode, needsLogging := resolveRecoveredExit(loggedErr, 3)

    if loggedErr != err {
        t.Fatalf("expected the same error back")
    }

    if 3 != exitCode {
        t.Fatalf("expected the caller's exit code, got %d", exitCode)
    }

    if true == needsLogging {
        t.Fatalf("expected no second logging")
    }
}

func TestResolveRecoveredExit_UnloggedErrorNeedsLogging(t *testing.T) {
    plainErr := exception.NewError("fresh", nil, nil)

    err, exitCode, needsLogging := resolveRecoveredExit(plainErr, 4)

    if plainErr != err || 4 != exitCode || false == needsLogging {
        t.Fatalf("expected the fresh error back with the caller's code and a pending log")
    }
}

func TestResolveRecoveredExit_WrapsAForeignErrorAndAPlainValue(t *testing.T) {
    foreignErr, _, foreignNeedsLogging := resolveRecoveredExit(io.ErrUnexpectedEOF, 5)
    if nil == foreignErr || false == strings.Contains(foreignErr.Error(), io.ErrUnexpectedEOF.Error()) || false == foreignNeedsLogging {
        t.Fatalf("expected the foreign error to be wrapped and logged, got %v", foreignErr)
    }

    valueErr, _, valueNeedsLogging := resolveRecoveredExit("boom", 5)
    if nil == valueErr || false == strings.Contains(valueErr.Error(), "panic") || false == valueNeedsLogging {
        t.Fatalf("expected the plain value to become a panic error, got %v", valueErr)
    }
}
