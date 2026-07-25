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
        return printErr
    }

    return envelopeExitError(envelope)
}

/* the rendered envelope is the command result, so an envelope reporting a failure has to leave the process with a non-zero status: a deployment gate such as `app debug:container app.repository.order || exit 1` is otherwise passed by a service that does not resolve. The returned error is marked as logged because the rendered envelope already carries the full report. */
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

    _ = exception.MarkLogged(reportedErr)

    return exception.NewExitError(1, reportedErr)
}
