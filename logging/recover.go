package logging

import (
    "fmt"
    "os"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* LogOnRecover recovers the panic in flight, logs it, and panics again with the same value when panicAgain is set. It never terminates the process. The helper is written to be installed with defer, which places it above every defer registered before it — the container teardown, the scope closes, the shutdown hooks — and a process exit from there would skip all of them, so the one thing a logging helper must not do is take the exit itself. A recovered *exception.ExitError is therefore logged like any other error and, under panicAgain, re-panicked unchanged so its exit code reaches whoever owns the process boundary; LogOnRecoverAndExit is the helper named for taking that exit. */
func LogOnRecover(
    logger loggingcontract.Logger,
    panicAgain bool,
) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    exitError, isExitError := recoveredValue.(*exception.ExitError)
    if true == isExitError {
        err := exitError.ErrorValue()

        if nil != err && false == err.AlreadyLogged() {
            LogError(logger, err)
            err.MarkAsLogged()
        }

        if true == panicAgain {
            /* the wrapper is re-panicked rather than the error it carries: the exit code lives on the wrapper, and dropping it here would silently turn a deliberate exit code into the generic one an outer handler falls back to */
            exception.Exit(exitError)
        }

        return
    }

    if err, ok := recoveredValue.(*exception.Error); true == ok {
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
        err = value

    case error:
        err = exception.NewError(
            value.Error(),
            nil,
            value,
        )

    default:
        err = exception.NewError(
            "panic",
            map[string]any{
                "value": value,
            },
            nil,
        )
    }

    LogError(logger, err)

    if true == panicAgain {
        err.MarkAsLogged()

        exception.Panic(err)
    }
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

    if true == needsLogging {
        LogError(logger, err)
        err.MarkAsLogged()
    }

    if nil != beforeExit {
        beforeExit()
    }

    /* @important the earlier logging may have gone to a file logger, leaving stdout/stderr silent: without this echo a fatal exit (e.g. an http bind failure logged by runHttp) terminates the process with no visible trace in a container whose logs are the standard streams */
    echoExitToStderr(err, resolvedExitCode)

    os.Exit(resolvedExitCode)
}

/* resolveRecoveredExit normalizes a recovered value into the error the exit reports, the exit code the process takes, and whether that error still needs logging. An ExitError carries its own code; one holding no error value — the zero value is constructible outside the constructor that refuses nil — is given an error naming the anomaly instead of dereferencing nil inside the one handler that must not panic. An error already logged is not logged again. */
func resolveRecoveredExit(
    recovered any,
    exitCode int,
) (*exception.Error, int, bool) {
    exitError, isExitError := recovered.(*exception.ExitError)
    if true == isExitError {
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

    if err, isError := recovered.(*exception.Error); true == isError {
        if true == err.AlreadyLogged() {
            return err, exitCode, false
        }
    }

    var err *exception.Error

    switch value := recovered.(type) {
    case *exception.Error:
        err = value

    case error:
        err = exception.NewError(
            value.Error(),
            nil,
            value,
        )

    default:
        err = exception.NewError(
            "panic",
            map[string]any{
                "value": value,
            },
            nil,
        )
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
