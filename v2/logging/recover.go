package logging

import (
    "os"

    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
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
            os.Exit(exitError.ExitCode())
        }

        LogError(logger, err)
        err.MarkAsLogged()

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
            os.Exit(exitError.ExitCode())
        }

        LogError(logger, err)
        err.MarkAsLogged()

        os.Exit(exitError.ExitCode())
    }

    if err, ok := recovered.(*exception.Error); true == ok {
        if true == err.AlreadyLogged() {
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

    os.Exit(exitCode)
}
