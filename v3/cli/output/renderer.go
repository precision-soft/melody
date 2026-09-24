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
        /* the envelope's own failure survives a printing failure, which would otherwise replace the reason the command failed */
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

/* the rendered envelope is the command result, so an envelope reporting a failure leaves the process with a non-zero status, which a deployment gate such as `app debug:container app.repository.order || exit 1` relies on. It is returned unmarked so the exit path writes it to the application log. */
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
