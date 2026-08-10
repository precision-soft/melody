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

/* debugErrorMessage renders an error's text for the debug-mode response body under a recover: the sites that consult it run inside the kernel's recovery defer, where a value whose Error() panics would raise a second panic past the recovery and reset the connection instead of serving the degraded page — the same trade exception.LogContext makes for the record's text. */
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
