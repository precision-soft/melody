package logging

import (
    "bytes"
    "errors"
    "io"
    "os"
    "os/exec"
    "strings"
    "testing"
    "time"

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

func TestResolveRecoveredExit_ZeroValueExitErrorIsNotHonoredAsAnExit(t *testing.T) {
    err, exitCode, needsLogging := resolveRecoveredExit(&exception.ExitError{}, 1)

    if nil == err {
        t.Fatalf("expected a substitute error for an exit error carrying an out-of-range code")
    }

    if false == strings.Contains(err.Error(), "exit error carries an out-of-range exit code") {
        t.Fatalf("expected the substitute error to name the anomaly, got %q", err.Error())
    }

    if 1 != exitCode {
        t.Fatalf("expected the caller's exit code instead of the zero value's own 0, got %d", exitCode)
    }

    if false == needsLogging {
        t.Fatalf("expected the anomaly to be logged")
    }
}

func TestResolveRecoveredExit_OutOfRangeExitErrorCarriesItsErrorValueAsTheCause(t *testing.T) {
    err, exitCode, needsLogging := resolveRecoveredExit(&exception.ExitError{}, 3)

    if 3 != exitCode {
        t.Fatalf("expected the caller's exit code, got %d", exitCode)
    }

    if nil == err || false == needsLogging {
        t.Fatalf("expected a substitute error pending logging")
    }

    /* the zero value carries no error value, so the substitute must not box a typed nil as its cause */
    if nil != err.Unwrap() {
        t.Fatalf("expected no cause for a wrapper carrying no error value, got %v", err.Unwrap())
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

func TestLogOnRecover_AlreadyLoggedHttpException_ProducesNoSecondRecord(t *testing.T) {
    httpException := exception.NewHttpException(500, "already reported")

    _ = exception.MarkLogged(httpException)

    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic(httpException)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected the mark to suppress the record, got %d record(s): %q", logger.calls, logger.lastMessage)
    }
}

func TestLogOnRecover_AlreadyLoggedHttpException_RePanicsTheMarkedValueUnchanged(t *testing.T) {
    httpException := exception.NewHttpException(500, "already reported")

    _ = exception.MarkLogged(httpException)

    logger := &captureLogger{}

    var rePanicked any

    func() {
        defer func() {
            rePanicked = recover()
        }()

        defer LogOnRecover(logger, true)

        panic(httpException)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected no record, got %d", logger.calls)
    }

    rePanickedHttpException, isHttpException := rePanicked.(*exception.HttpException)
    if false == isHttpException {
        t.Fatalf("expected the http exception to be re-panicked unchanged, got %T", rePanicked)
    }

    if httpException != rePanickedHttpException {
        t.Fatalf("expected the very value that carries the mark, got a different instance")
    }

    if false == rePanickedHttpException.AlreadyLogged() {
        t.Fatalf("expected the re-panicked value to still carry the mark")
    }
}

func TestResolveRecoveredExit_AlreadyLoggedHttpExceptionIsNotLoggedAgain(t *testing.T) {
    httpException := exception.NewHttpException(500, "already reported")

    _ = exception.MarkLogged(httpException)

    err, exitCode, needsLogging := resolveRecoveredExit(httpException, 9)

    if true == needsLogging {
        t.Fatalf("expected the mark to spare the record at the exit boundary")
    }

    if 9 != exitCode {
        t.Fatalf("expected the caller's exit code, got %d", exitCode)
    }

    if nil == err {
        t.Fatalf("expected an error for the stderr echo even when the record is spared")
    }
}

func TestLogOnRecover_UnmarkedHttpException_IsStillLogged(t *testing.T) {
    httpException := exception.NewHttpException(500, "never reported")

    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        panic(httpException)
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one record for an unmarked http exception, got %d", logger.calls)
    }
}

func TestLogOnRecoverAndExitAfter_RefusesAnOutOfRangeExitCode(t *testing.T) {
    for _, outOfRangeCode := range []int{0, -1, 256} {
        func() {
            defer func() {
                recoveredValue := recover()
                if nil == recoveredValue {
                    t.Fatalf("expected the out-of-range exit code %d to be refused", outOfRangeCode)
                }

                recoveredErr, isError := recoveredValue.(error)
                if false == isError || false == strings.Contains(recoveredErr.Error(), "exit code out of range") {
                    t.Fatalf("expected the refusal to name the rule for code %d, got %v", outOfRangeCode, recoveredValue)
                }
            }()

            /* nil recovered: the refusal must fire on the healthy pass, so a caller wired with a bad code is caught on its first run rather than on its first panic */
            LogOnRecoverAndExitAfter(&captureLogger{}, nil, outOfRangeCode, nil)
        }()
    }
}

func TestLogOnRecoverAndExitAfter_AcceptsTheRangeBoundsOnTheHealthyPass(t *testing.T) {
    for _, validCode := range []int{1, 255} {
        LogOnRecoverAndExitAfter(&captureLogger{}, nil, validCode, nil)
    }
}

/* the marker tells a re-executed test binary that it is the child taking the exit for a zero-value exit error */
const zeroValueExitErrorProbeMarker = "MELODY_ZERO_VALUE_EXIT_ERROR_PROBE"

func TestLogOnRecoverAndExitAfter_ZeroValueExitErrorExitsWithTheCallersCode(t *testing.T) {
    if "1" == os.Getenv(zeroValueExitErrorProbeMarker) {
        LogOnRecoverAndExitAfter(
            &captureLogger{},
            &exception.ExitError{},
            1,
            nil,
        )

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestLogOnRecoverAndExitAfter_ZeroValueExitErrorExitsWithTheCallersCode$",
    )
    command.Env = append(os.Environ(), zeroValueExitErrorProbeMarker+"=1")

    output, runErr := command.CombinedOutput()

    var exitErr *exec.ExitError
    if false == errors.As(runErr, &exitErr) {
        t.Fatalf("expected the child to exit non-zero instead of reporting success, got %v with output %q", runErr, string(output))
    }

    if 1 != exitErr.ExitCode() {
        t.Fatalf("expected the caller's exit code 1 instead of the zero value's own 0, got %d with output %q", exitErr.ExitCode(), string(output))
    }

    if false == strings.Contains(string(output), "melody: exiting with code 1") {
        t.Fatalf("expected the stderr echo for the normalized exit, got %q", string(output))
    }
}

func TestRunExitStepShielded_AbandonsAStepThatDoesNotReturn(t *testing.T) {
    originalBudget := exitStepBudget
    exitStepBudget = 30 * time.Millisecond
    defer func() {
        exitStepBudget = originalBudget
    }()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    helperDone := make(chan struct{})

    go func() {
        defer close(helperDone)

        runExitStepShielded("running the probe teardown", func() {
            select {}
        })
    }()

    select {
    case <-helperDone:

    case <-time.After(2 * time.Second):
        os.Stderr = originalStderr
        t.Fatalf("expected the shielded step to be abandoned within the budget; the exit handler would hang")
    }

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if false == strings.Contains(string(output), "running the probe teardown did not return within") {
        t.Fatalf("expected the abandonment line on stderr, got %q", string(output))
    }
}

func TestRunExitStepShielded_ReportsNothingForAStepThatReturns(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    stepRan := false
    runExitStepShielded("running the probe teardown", func() {
        stepRan = true
    })

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if false == stepRan {
        t.Fatalf("expected the step to run")
    }

    if "" != string(output) {
        t.Fatalf("expected no stderr line for a step that returned, got %q", string(output))
    }
}

func TestWriteExitCertificate_WritesAnEmergencyRecordWithTheExitCode(t *testing.T) {
    capture := &captureLogger{}

    err := exception.NewError("fatal failure", nil, nil)
    _ = exception.MarkLogged(err)

    writeExitCertificate(capture, err, 3)

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    if loggingcontract.LevelEmergency != capture.lastLevel {
        t.Fatalf("expected the certificate at emergency level, got %s", capture.lastLevel)
    }

    if "process exiting after unrecovered error" != capture.lastMessage {
        t.Fatalf("unexpected certificate message %q", capture.lastMessage)
    }

    if 3 != capture.lastContext["exitCode"] {
        t.Fatalf("expected the exit code on the certificate, got %v", capture.lastContext["exitCode"])
    }

    if "fatal failure" != capture.lastContext["error"] {
        t.Fatalf("expected the error on the certificate, got %v", capture.lastContext["error"])
    }
}

func TestWriteExitCertificate_PassesAnEmergencyOnlyThreshold(t *testing.T) {
    buffer := &bytes.Buffer{}
    logger := NewJsonLogger(buffer, loggingcontract.LevelEmergency)

    writeExitCertificate(logger, exception.NewError("fatal failure", nil, nil), 1)

    if false == strings.Contains(buffer.String(), "process exiting after unrecovered error") {
        t.Fatalf("expected the certificate to pass the emergency threshold, got %q", buffer.String())
    }
}

/* the marker tells a re-executed test binary that it is the child taking the exit whose certificate is asserted */
const exitCertificateProbeMarker = "MELODY_EXIT_CERTIFICATE_PROBE"

func TestLogOnRecoverAndExitAfter_WritesTheCertificateForAnAlreadyLoggedError(t *testing.T) {
    if "1" == os.Getenv(exitCertificateProbeMarker) {
        err := exception.NewError("boom", nil, nil)
        LogError(EmergencyLogger(), err)
        err.MarkAsLogged()

        LogOnRecoverAndExit(EmergencyLogger(), err, 7)

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestLogOnRecoverAndExitAfter_WritesTheCertificateForAnAlreadyLoggedError$",
    )
    command.Env = append(os.Environ(), exitCertificateProbeMarker+"=1")

    output, runErr := command.CombinedOutput()

    var exitErr *exec.ExitError
    if false == errors.As(runErr, &exitErr) {
        t.Fatalf("expected the child to exit non-zero, got %v with output %q", runErr, string(output))
    }

    if 7 != exitErr.ExitCode() {
        t.Fatalf("expected exit code 7, got %d with output %q", exitErr.ExitCode(), string(output))
    }

    if false == strings.Contains(string(output), "process exiting after unrecovered error") {
        t.Fatalf("expected the exit certificate for an already-logged error, got %q", string(output))
    }
}
