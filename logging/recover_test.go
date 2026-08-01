package logging

import (
    "errors"
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

/* @info the sibling exit path names this anomaly and logs it; the recover helper used to skip the logging step entirely for an exit wrapper carrying no error value, returning with no record from the helper whose purpose is the record */
func TestLogOnRecover_ZeroValueExitErrorLogsTheAnomaly(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic(&exception.ExitError{})
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one record for the anomaly, got %d", logger.calls)
    }

    if false == strings.Contains(logger.lastMessage, "exit requested with no error value") {
        t.Fatalf("expected the anomaly to be named, got %q", logger.lastMessage)
    }
}

/* @info a typed-nil exception or exit wrapper is the value someone panicked with, not an exception to dereference: both type asserts used to accept it and the next method call — AlreadyLogged locks a nil receiver, ErrorValue reads through it — panicked inside the recover handler */
func TestLogOnRecover_TypedNilPanicValuesAreLoggedAsPanics(t *testing.T) {
    for _, panicValue := range []any{(*exception.Error)(nil), (*exception.ExitError)(nil)} {
        logger := &captureLogger{}

        func() {
            defer LogOnRecover(logger, false)

            panic(panicValue)
        }()

        if 1 != logger.calls {
            t.Fatalf("expected one record for %T, got %d", panicValue, logger.calls)
        }

        if "panic" != logger.lastMessage {
            t.Fatalf("expected the plain panic record for %T, got %q", panicValue, logger.lastMessage)
        }
    }
}

/* @info the mark travels with the record, not with the re-panic: leaving a logged error unmarked under panicAgain false let any later handler holding the same instance — a memoized container failure is shared by design — record it a second time */
func TestLogOnRecover_MarksLoggedWithoutPanicAgain(t *testing.T) {
    err := exception.NewError("boom", nil, nil)

    func() {
        defer LogOnRecover(&captureLogger{}, false)

        exception.Panic(err)
    }()

    if false == err.AlreadyLogged() {
        t.Fatalf("expected the logged error to be marked")
    }
}

/* @info the deferred handler runs with the panicking frames still on the stack — the only moment the origin of a runtime panic can still be captured for the record; every other recovery boundary in the framework writes it, and these helpers were the ones that did not */
func TestLogOnRecover_ForeignPanicCarriesThePanicStack(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic(errors.New("boom"))
    }()

    stackValue, hasStack := logger.lastContext["panicStack"]
    if false == hasStack {
        t.Fatalf("expected the panic stack in the record, got %v", logger.lastContext)
    }

    stackText, isString := stackValue.(string)
    if false == isString || false == strings.Contains(stackText, "TestLogOnRecover_ForeignPanicCarriesThePanicStack") {
        t.Fatalf("expected the stack to name the panic site")
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

/* @info a typed-nil exception or exit wrapper panicked into the exit handler used to be dereferenced by the type asserts — ErrorValue on a nil wrapper, AlreadyLogged on a nil receiver — replacing the deliberate exit with a second panic; it now normalizes as the plain panic value it is, under the caller's exit code */
func TestResolveRecoveredExit_TypedNilValuesNormalizeAsPanics(t *testing.T) {
    for _, recoveredValue := range []any{(*exception.Error)(nil), (*exception.ExitError)(nil)} {
        err, exitCode, needsLogging := resolveRecoveredExit(recoveredValue, 7)

        if nil == err || "panic" != err.Message() {
            t.Fatalf("expected the plain panic record for %T, got %v", recoveredValue, err)
        }

        if 7 != exitCode {
            t.Fatalf("expected the caller's exit code for %T, got %d", recoveredValue, exitCode)
        }

        if false == needsLogging {
            t.Fatalf("expected the anomaly to be logged for %T", recoveredValue)
        }
    }
}

/* @info the foreign error keeps its panic stack: the normalization runs inside the deferred exit handler, with the panicking frames still on the stack, and the record is the only place they survive */
func TestResolveRecoveredExit_ForeignErrorCarriesThePanicStack(t *testing.T) {
    err, _, _ := resolveRecoveredExit(errors.New("boom"), 5)

    if nil == err {
        t.Fatalf("expected an error")
    }

    stackValue, hasStack := err.Context()["panicStack"]
    if false == hasStack {
        t.Fatalf("expected the panic stack in the context")
    }

    if stackText, isString := stackValue.(string); false == isString || "" == stackText {
        t.Fatalf("expected a non-empty stack text")
    }
}

/* the marker tells a re-executed test binary that it is the child that exercises the shielded exit handler rather than the parent that watches its exit status */
const exitHandlerShieldProbeMarker = "MELODY_EXIT_HANDLER_SHIELD_PROBE"

type panickingProbeLogger struct{}

func (instance *panickingProbeLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    panic("logger died while writing the record")
}

func (instance *panickingProbeLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *panickingProbeLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *panickingProbeLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *panickingProbeLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *panickingProbeLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

/* @info the subprocess is the assertion: the shields exist so that a panic in the before-exit hook or in the logger costs its own step and never the exit code — before them, the child died with the Go runtime's code 2, no stderr echo, no os.Exit. The parent reads the child's exit status, which is the only place the difference is visible. */
func TestLogOnRecoverAndExitAfter_ShieldsTheHookAndTheRecord(t *testing.T) {
    if "hook" == os.Getenv(exitHandlerShieldProbeMarker) {
        LogOnRecoverAndExitAfter(
            &captureLogger{},
            exception.NewExitError(9, exception.NewError("boom", nil, nil)),
            1,
            func() {
                panic("teardown died")
            },
        )

        return
    }

    if "logger" == os.Getenv(exitHandlerShieldProbeMarker) {
        LogOnRecoverAndExitAfter(
            &panickingProbeLogger{},
            exception.NewExitError(9, exception.NewError("boom", nil, nil)),
            1,
            nil,
        )

        return
    }

    for _, probeMode := range []string{"hook", "logger"} {
        command := exec.Command(
            os.Args[0],
            "-test.run=^TestLogOnRecoverAndExitAfter_ShieldsTheHookAndTheRecord$",
        )
        command.Env = append(os.Environ(), exitHandlerShieldProbeMarker+"="+probeMode)

        output, runErr := command.CombinedOutput()

        var exitErr *exec.ExitError
        if false == errors.As(runErr, &exitErr) {
            t.Fatalf("[%s] expected the child to exit non-zero, got %v with output %q", probeMode, runErr, string(output))
        }

        if 9 != exitErr.ExitCode() {
            t.Fatalf("[%s] expected the deliberate exit code 9 to survive the panic, got %d with output %q", probeMode, exitErr.ExitCode(), string(output))
        }

        if false == strings.Contains(string(output), "melody: exiting with code 9") {
            t.Fatalf("[%s] expected the stderr echo to survive the panic, got %q", probeMode, string(output))
        }

        if false == strings.Contains(string(output), "melody: panic while") {
            t.Fatalf("[%s] expected the shielded step to report its own panic, got %q", probeMode, string(output))
        }
    }
}

/* the marker tells a re-executed test binary that it is the child taking the exit that LogOnRecoverAndExit is named for */
const logOnRecoverAndExitProbeMarker = "MELODY_LOG_ON_RECOVER_AND_EXIT_PROBE"

/* @info the two-argument helper is the one an owner of the process boundary installs when it has no teardown to run between the record and the exit, and nothing had ever entered it: it must take the exit code the recovered value carries, exactly as the four-argument form does, and it can only be observed from a child process because the exit is the point */
func TestLogOnRecoverAndExit_TakesTheExitCodeOfTheRecoveredValue(t *testing.T) {
    if "1" == os.Getenv(logOnRecoverAndExitProbeMarker) {
        LogOnRecoverAndExit(
            &captureLogger{},
            exception.NewExitError(9, exception.NewError("boom", nil, nil)),
            1,
        )

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestLogOnRecoverAndExit_TakesTheExitCodeOfTheRecoveredValue$",
    )
    command.Env = append(os.Environ(), logOnRecoverAndExitProbeMarker+"=1")

    output, runErr := command.CombinedOutput()

    var exitErr *exec.ExitError
    if false == errors.As(runErr, &exitErr) {
        t.Fatalf("expected the child to exit non-zero, got %v with output %q", runErr, string(output))
    }

    if 9 != exitErr.ExitCode() {
        t.Fatalf("expected the carried exit code 9, got %d with output %q", exitErr.ExitCode(), string(output))
    }

    if false == strings.Contains(string(output), "melody: exiting with code 9") {
        t.Fatalf("expected the stderr echo, got %q", string(output))
    }
}

/* @info the handler is installed with defer and therefore runs on every return, panicking or not; a nil recovered value is the ordinary return, and taking the exit for it would end the process on success */
func TestLogOnRecoverAndExitAfter_WithoutAPanic_DoesNothing(t *testing.T) {
    logger := &captureLogger{}

    hookRan := false

    LogOnRecoverAndExitAfter(logger, nil, 1, func() {
        hookRan = true
    })

    if 0 != logger.calls {
        t.Fatalf("expected no record without a panic, got %d", logger.calls)
    }

    if true == hookRan {
        t.Fatalf("expected the before-exit hook not to run without a panic")
    }
}

/* @info an already-logged error is re-panicked without a second record: the re-panic is what carries the failure to the owner of the process boundary, and logging it again would produce two records of one failure — the branch that returns early had never been entered with panicAgain set */
func TestLogOnRecover_AlreadyLoggedException_RePanicsWithoutASecondRecord(t *testing.T) {
    logger := &captureLogger{}

    alreadyLogged := exception.NewError("boom", nil, nil)
    alreadyLogged.MarkAsLogged()

    defer func() {
        recoveredValue := recover()

        if alreadyLogged != recoveredValue {
            t.Fatalf("expected the same error to be re-panicked, got %#v", recoveredValue)
        }

        if 0 != logger.calls {
            t.Fatalf("expected no second record for an already-logged error, got %d", logger.calls)
        }
    }()

    func() {
        defer LogOnRecover(logger, true)

        panic(alreadyLogged)
    }()
}

/* @info a typed-nil foreign error is the value someone panicked with, not an error to describe: reading its message dereferences the nil receiver inside the handler that must not panic, so it is recorded as a plain panic payload — the same answer the typed-nil exception shapes get */
func TestLogOnRecover_TypedNilForeignError_IsLoggedAsAPanicValue(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic(error((*typedNilProbeError)(nil)))
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one record, got %d", logger.calls)
    }

    if "panic" != logger.lastMessage {
        t.Fatalf("expected the plain panic record, got %q", logger.lastMessage)
    }

    if nil == logger.lastContext["panicStack"] {
        t.Fatalf("expected the panic stack to travel with the record")
    }
}

/* @info the same typed nil reaching the exit path normalizes the same way; the two switches are written twice, so they can drift apart */
func TestResolveRecoveredExit_TypedNilForeignError_NormalizesAsAPanicValue(t *testing.T) {
    err, exitCode, needsLogging := resolveRecoveredExit(error((*typedNilProbeError)(nil)), 4)

    if nil == err || "panic" != err.Message() {
        t.Fatalf("expected the plain panic record, got %v", err)
    }

    if 4 != exitCode {
        t.Fatalf("expected the caller's exit code, got %d", exitCode)
    }

    if false == needsLogging {
        t.Fatalf("expected the anomaly to still need logging")
    }
}

/* @info a panic with a value that is not an error at all — a string from a third-party library, the runtime's own plain values — must still produce a record carrying the value and the frames, because that is all there is to go on */
func TestLogOnRecover_PlainValuePanic_CarriesTheValueAndTheStack(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic("a plain string")
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one record, got %d", logger.calls)
    }

    if "a plain string" != logger.lastContext["value"] {
        t.Fatalf("expected the panic value in the context, got %v", logger.lastContext["value"])
    }

    if nil == logger.lastContext["panicStack"] {
        t.Fatalf("expected the panic stack to travel with the record")
    }
}
