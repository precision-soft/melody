package logging

import (
    "fmt"
    "os"
    "runtime/debug"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* LogOnRecover recovers the panic in flight, logs it, and panics again when panicAgain is set. It never terminates the process: installed with defer it sits above the container teardown, the scope closes and the shutdown hooks, which an exit taken here would skip. A recovered *exception.ExitError is therefore re-panicked unchanged so its code reaches the owner of the process boundary — LogOnRecoverAndExit is the helper that takes the exit. */
func LogOnRecover(
    logger loggingcontract.Logger,
    panicAgain bool,
) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    exitError, isExitError := recoveredValue.(*exception.ExitError)
    if true == isExitError && nil != exitError {
        err := exitError.ErrorValue()
        if nil == err {
            err = exception.NewError(
                "exit requested with no error value",
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                nil,
            )
        }

        if false == err.AlreadyLogged() {
            LogError(logger, err)
            err.MarkAsLogged()
        }

        if true == panicAgain {
            /* the wrapper is re-panicked rather than the error it carries: the exit code lives on the wrapper */
            exception.Exit(exitError)
        }

        return
    }

    if true == isAlreadyLoggedValue(recoveredValue) {
        if true == panicAgain {
            /* the recovered value is re-panicked unchanged rather than rebuilt: the mark that suppressed this record lives on it, and a fresh wrapper would carry none */
            panic(recoveredValue)
        }

        return
    }

    var err *exception.Error

    switch value := recoveredValue.(type) {
    case *exception.Error:
        if nil == value {
            err = newRecoveredPanicError(value)
        } else {
            err = value
        }

    case *exception.ExitError:
        /* only the typed nil reaches this case: the non-nil wrapper returned above */
        err = newRecoveredPanicError(value)

    case error:
        if true == internal.IsNilInterface(value) {
            err = newRecoveredPanicError(value)
        } else {
            err = exception.NewError(
                value.Error(),
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                value,
            )
        }

    default:
        err = newRecoveredPanicError(value)
    }

    LogError(logger, err)
    err.MarkAsLogged()

    if true == panicAgain {
        exception.Panic(err)
    }
}

/* newRecoveredPanicError wraps a panic payload that carries no usable error together with the stack of the panic still in flight: the deferred handler runs with the panicking frames intact, and this is the only moment the origin of a runtime panic can be captured. */
func newRecoveredPanicError(value any) *exception.Error {
    return exception.NewError(
        "panic",
        map[string]any{
            "value":      value,
            "panicStack": string(debug.Stack()),
        },
        nil,
    )
}

func LogOnRecoverAndExit(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
) {
    LogOnRecoverAndExitAfter(logger, recovered, exitCode, nil)
}

/* LogOnRecoverAndExitAfter logs the recovered value like LogOnRecoverAndExit and runs beforeExit between the record and the process exit. It is the one place that is both after the record and before the exit: a teardown deferred below never runs, because os.Exit skips it, and one run before closes the logger the final record must travel through. */
func LogOnRecoverAndExitAfter(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
    beforeExit func(),
) {
    if nil == recovered {
        return
    }

    err, resolvedExitCode, needsLogging := resolveRecoveredExit(recovered, exitCode)

    /* every step between the recovery and the exit runs under its own recover: this is the last handler of the process, so a second panic must cost only its own step, never the resolved exit code, the stderr echo or os.Exit itself */
    if true == needsLogging {
        runExitStepShielded("logging the exit record", func() {
            LogError(logger, err)
            err.MarkAsLogged()
        })
    }

    if nil != beforeExit {
        runExitStepShielded("running the before-exit hook", func() {
            beforeExit()
        })
    }

    /* the earlier record may have gone to a file logger, leaving a container whose logs are the standard streams with no trace of a fatal exit */
    echoExitToStderr(err, resolvedExitCode)

    os.Exit(resolvedExitCode)
}

/* runExitStepShielded contains a panic inside one step of the exit handler and echoes it to stderr best-effort. */
func runExitStepShielded(stepName string, step func()) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        _, _ = fmt.Fprintf(os.Stderr, "melody: panic while %s during the exit handler: %v\n", stepName, recoveredValue)
    }()

    step()
}

/* resolveRecoveredExit normalizes a recovered value into the error the exit reports, the exit code the process takes, and whether that error still needs logging. An ExitError carries its own code; one holding no error value is given an error naming the anomaly instead of dereferencing nil inside the one handler that must not panic. A typed-nil exception is the value someone panicked with and is normalized as a plain panic value under the caller's code. */
func resolveRecoveredExit(
    recovered any,
    exitCode int,
) (*exception.Error, int, bool) {
    exitError, isExitError := recovered.(*exception.ExitError)
    if true == isExitError && nil != exitError {
        err := exitError.ErrorValue()

        if nil == err {
            err = exception.NewError(
                "exit requested with no error value",
                nil,
                nil,
            )

            return err, exitError.ExitCode(), true
        }

        if true == err.AlreadyLogged() {
            return err, exitError.ExitCode(), false
        }

        return err, exitError.ExitCode(), true
    }

    alreadyLogged := isAlreadyLoggedValue(recovered)

    var err *exception.Error

    switch value := recovered.(type) {
    case *exception.Error:
        if nil == value {
            err = newRecoveredPanicError(value)
        } else {
            err = value
        }

    case *exception.ExitError:
        /* only the typed nil reaches this case: the non-nil wrapper returned above */
        err = newRecoveredPanicError(value)

    case error:
        if true == internal.IsNilInterface(value) {
            err = newRecoveredPanicError(value)
        } else {
            err = exception.NewError(
                value.Error(),
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                value,
            )
        }

    default:
        err = newRecoveredPanicError(value)
    }

    return err, exitCode, false == alreadyLogged
}

/* isAlreadyLoggedValue answers whether a recovered panic payload already carries the logged mark. A payload that is not an error carries none; everything else goes to exception.IsAlreadyLogged, so the recover helpers read the mark at the depth MarkLogged writes it. */
func isAlreadyLoggedValue(recovered any) bool {
    err, isError := recovered.(error)
    if false == isError {
        return false
    }

    return exception.IsAlreadyLogged(err)
}

/* echoExitToStderr writes one final line before a fatal exit so a non-zero exit is never completely silent on the standard streams, whatever destination the configured logger has. */
func echoExitToStderr(err error, exitCode int) {
    if 0 == exitCode {
        return
    }

    message := "-"
    if nil != err {
        message = err.Error()
    }

    _, _ = fmt.Fprintf(os.Stderr, "melody: exiting with code %d after unrecovered error: %s\n", exitCode, message)
}
