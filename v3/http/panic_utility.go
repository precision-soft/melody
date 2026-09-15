package http

import (
    "fmt"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

func RecoverToError(recoveredValue any) error {
    if nil == recoveredValue {
        return nil
    }

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
