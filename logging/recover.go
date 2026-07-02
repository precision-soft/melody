package logging

import (
    "fmt"
    "os"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func LogOnRecover(
    logger loggingcontract.Logger,
    panicAgain bool,
) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    exitError, ok := recoveredValue.(*exception.ExitError)
    if true == ok {
        err := exitError.ErrorValue()

        if true == err.AlreadyLogged() {
            echoExitToStderr(err, exitError.ExitCode())

            os.Exit(exitError.ExitCode())
        }

        LogError(logger, err)
        err.MarkAsLogged()

        echoExitToStderr(err, exitError.ExitCode())

        os.Exit(exitError.ExitCode())
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
    if nil == recovered {
        return
    }

    exitError, ok := recovered.(*exception.ExitError)
    if true == ok {
        err := exitError.ErrorValue()

        if true == err.AlreadyLogged() {
            echoExitToStderr(err, exitError.ExitCode())

            os.Exit(exitError.ExitCode())
        }

        LogError(logger, err)
        err.MarkAsLogged()

        echoExitToStderr(err, exitError.ExitCode())

        os.Exit(exitError.ExitCode())
    }

    if err, ok := recovered.(*exception.Error); true == ok {
        if true == err.AlreadyLogged() {
            /* @important the earlier logging may have gone to a file logger, leaving stdout/stderr silent: without this echo a fatal exit (e.g. an http bind failure logged by runHttp) terminates the process with no visible trace in a container whose logs are the standard streams */
            echoExitToStderr(err, exitCode)

            os.Exit(exitCode)
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

    LogError(logger, err)
    err.MarkAsLogged()

    echoExitToStderr(err, exitCode)

    os.Exit(exitCode)
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
