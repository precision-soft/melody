package internal

import (
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

    stringValue, isString := value.(string)
    if true == isString {
        context["value"] = stringValue
    }

    return exception.NewError(message, context, causeErr)
}
