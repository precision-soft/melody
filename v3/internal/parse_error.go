package internal

import (
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func ParseError(
    parameterName string,
    expectedType string,
    value any,
    causeErr error,
) *exception.Error {
    message := "parameter is not a '" + expectedType + "'"
    if nil != causeErr {
        message = "parameter is not a valid '" + expectedType + "'"
    }

    context := map[string]any{
        "parameterName": parameterName,
        "expectedType":  expectedType,
        "actualType":    StringifyType(value),
    }

    /* the refused value enters the context for the scalar shapes; a secret parameter withholds the whole error from the log on the config path */
    switch typedValue := value.(type) {
    case string:
        context["value"] = typedValue
    case int, int64, float32, float64, time.Duration:
        context["value"] = typedValue
    }

    return exception.NewError(message, context, causeErr)
}
