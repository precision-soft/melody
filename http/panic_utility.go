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

    /* a typed-nil error is normalized to the generic branch, as the exit handler's resolver does: passed through, its Error() would dereference a nil receiver in the first reader without a guard, a second panic inside the recovery defer. */
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

/* debugErrorMessage renders an error's text for the debug-mode body under a recover: a value whose Error() panics would raise a second panic while the first is rendered, resetting the connection inside the kernel's recovery defer and costing the whole debug payload inside the exception listener. A named rendering failure beats losing the report, the trade exception.LogContext makes. */
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
