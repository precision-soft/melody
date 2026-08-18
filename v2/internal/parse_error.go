package internal

import (
    "time"

    "github.com/precision-soft/melody/v2/exception"
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

    /* the refused value enters the context for the scalar shapes an operator needs to see — a refusal that names neither the value nor its magnitude cannot be acted on; on the config path a secret parameter withholds this whole error from the log, so nothing new leaks there */
    switch typedValue := value.(type) {
    case string:
        context["value"] = typedValue
    case int, int64, float32, float64, time.Duration:
        context["value"] = typedValue
    }

    return exception.NewError(message, context, causeErr)
}
