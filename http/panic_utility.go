package http

import (
    "fmt"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

func RecoverToError(recoveredValue any) error {
    if nil == recoveredValue {
        return nil
    }

    /* a typed-nil error is normalized to the generic branch the way the exit handler's resolver normalizes it: passed through as-is it reads as a non-nil error whose Error() dereferences a nil receiver, and the first reader without its own guard — the kernel's debug-mode message, inside the recovery defer — raised a second panic that escaped ServeHttp and reset the connection. */
    err, ok := recoveredValue.(error)
    if true == ok && false == internal.IsNilInterface(err) {
        return err
    }

    stringValue, ok := recoveredValue.(string)
    if true == ok {
        return exception.NewError(stringValue, map[string]any{}, nil)
    }

    return exception.NewError(
        "panic recovered",
        map[string]any{
            "value": fmt.Sprintf("%v", recoveredValue),
        },
        nil,
    )
}

/* debugErrorMessage renders an error's text for the debug-mode response body under a recover: a value whose Error() panics — the shape that dereferences exactly the nil field that produced the panic — would otherwise raise a second panic while the first one is being rendered. Inside the kernel's recovery defer that reset the connection; inside the exception listener it was absorbed one level up by the dispatcher, at the price of the whole debug payload the listener exists to build, so the client received the kernel's fallback body instead of the degraded page. The trade is the one exception.LogContext makes for the record's text: a named rendering failure beats losing the report. */
func debugErrorMessage(err error) (message string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        message = fmt.Sprintf("error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}
