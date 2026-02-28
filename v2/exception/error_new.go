package exception

import (
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

func NewEmergency(message string, context exceptioncontract.Context, causeErr error) *Error {
    return newWithLevel(
        message,
        context,
        causeErr,
        loggingcontract.LevelEmergency,
    )
}

func NewError(message string, context exceptioncontract.Context, causeErr error) *Error {
    return newWithLevel(
        message,
        context,
        causeErr,
        loggingcontract.LevelError,
    )
}

func NewWarning(message string, context exceptioncontract.Context, causeErr error) *Error {
    return newWithLevel(
        message,
        context,
        causeErr,
        loggingcontract.LevelWarning,
    )
}

func NewInfo(message string, context exceptioncontract.Context, causeErr error) *Error {
    return newWithLevel(
        message,
        context,
        causeErr,
        loggingcontract.LevelInfo,
    )
}

func newWithLevel(
    message string,
    context exceptioncontract.Context,
    causeErr error,
    level loggingcontract.Level,
) *Error {
    return &Error{
        message:       message,
        context:       copyStringMap(context),
        causeErr:      causeErr,
        level:         level,
        alreadyLogged: false,
    }
}
