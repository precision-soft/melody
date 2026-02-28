package http

import (
    "fmt"

    "github.com/precision-soft/melody/exception"
)

func RecoverToError(recoveredValue any) error {
    if nil == recoveredValue {
        return nil
    }

    err, ok := recoveredValue.(error)
    if true == ok {
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
