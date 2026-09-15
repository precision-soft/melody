package output

import (
    "io"

    "github.com/precision-soft/melody/v3/exception"
)

func Render(
    writer io.Writer,
    envelope Envelope,
    option Option,
) error {
    printer := SelectPrinter(option)

    printErr := printer.Print(writer, envelope, option)
    if nil != printErr {

        reportedErr := envelopeExitError(envelope)
        if nil == reportedErr {
            return printErr
        }

        return exception.NewError(
            "failed to print the command result that reports a failure",
            map[string]any{
                "printError": printErr.Error(),
            },
            reportedErr,
        )
    }

    return envelopeExitError(envelope)
}

func envelopeExitError(envelope Envelope) error {
    if nil == envelope.Error {
        return nil
    }

    context := map[string]any{
        "command":   envelope.Meta.Command,
        "errorCode": envelope.Error.Code,
    }

    for key, value := range envelope.Error.Details {
        if "" == key {
            continue
        }

        if "command" == key || "errorCode" == key {
            continue
        }

        context[key] = value
    }

    reportedErr := exception.NewError(
        envelope.Error.Message,
        context,
        nil,
    )

    return exception.NewExitError(1, reportedErr)
}
