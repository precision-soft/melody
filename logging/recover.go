package logging

import (
    "fmt"
    "os"
    "runtime/debug"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* LogOnRecover recovers the panic in flight, logs it, and panics again when panicAgain is set — with the recovered value when it already is an exception, with the exception built around it otherwise. It never terminates the process. The helper is written to be installed with defer, which places it above every defer registered before it — the container teardown, the scope closes, the shutdown hooks — and a process exit from there would skip all of them, so the one thing a logging helper must not do is take the exit itself. A recovered *exception.ExitError is therefore logged like any other error and, under panicAgain, re-panicked unchanged so its exit code reaches whoever owns the process boundary; LogOnRecoverAndExit is the helper named for taking that exit. One carrying no error value is logged as the anomaly it is instead of being skipped — the sibling exit path names the same anomaly, and a helper whose purpose is the record must not stay silent on the one shape that reaches it without one. A typed-nil exception or exit error recovered here is the value someone panicked with, not an exception to dereference: it is logged as a plain panic value, like any other non-error payload. */
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
            /* the wrapper is re-panicked rather than the error it carries: the exit code lives on the wrapper, and dropping it here would silently turn a deliberate exit code into the generic one an outer handler falls back to */
            exception.Exit(exitError)
        }

        return
    }

    if err, ok := recoveredValue.(*exception.Error); true == ok && nil != err {
        if true == err.AlreadyLogged() {
            if true == panicAgain {
                exception.Panic(err)
            }

            return
        }
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

/* newRecoveredPanicError wraps a panic payload that carries no usable error — a plain value, or a typed-nil error whose methods would dereference the nil it holds — together with the stack of the panic still in flight: the deferred handler runs with the panicking frames intact, and this is the only moment the origin of a runtime panic can still be captured for the record. */
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

/* LogOnRecoverAndExitAfter logs the recovered value like LogOnRecoverAndExit and runs beforeExit between the logging and the process exit. The hook exists for the owner of the process boundary: a teardown deferred below this helper would never run, because os.Exit skips it, and a teardown run before it closes the very logger the final record must be written through — the hook is the one place that is both after the record and before the exit. */
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

    /* @important every step between the recovery and the exit runs under its own recover: this is the last handler of the process, so a second panic — a logger over a broken writer, a teardown that dies inside a service Close — must cost only its own step, never the deliberate exit code. Without the shields the runtime replaced the resolved code with the generic panic exit and skipped both the stderr echo and os.Exit itself. */
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

    /* @important the earlier logging may have gone to a file logger, leaving stdout/stderr silent: without this echo a fatal exit (e.g. an http bind failure logged by runHttp) terminates the process with no visible trace in a container whose logs are the standard streams */
    echoExitToStderr(err, resolvedExitCode)

    os.Exit(resolvedExitCode)
}

/* runExitStepShielded contains a panic inside one step of the exit handler and echoes it to stderr best-effort: the step's failure is worth one line, the exit code and the echo behind it are worth protecting from the step. */
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

/* resolveRecoveredExit normalizes a recovered value into the error the exit reports, the exit code the process takes, and whether that error still needs logging. An ExitError carries its own code; one holding no error value — the zero value is constructible outside the constructor that refuses nil — is given an error naming the anomaly instead of dereferencing nil inside the one handler that must not panic. A typed-nil exception or exit error is the value someone panicked with, not an exception to dereference: it is normalized as a plain panic value under the caller's exit code. An error already logged is not logged again. */
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

    if err, isError := recovered.(*exception.Error); true == isError && nil != err {
        if true == err.AlreadyLogged() {
            return err, exitCode, false
        }
    }

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

    return err, exitCode, true
}

/* @important echoExitToStderr writes one final line to stderr before a fatal exit so the failure is visible even when the configured logger writes elsewhere (for example the example apps' file logger): a non-zero exit must never be completely silent on the standard streams */
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
