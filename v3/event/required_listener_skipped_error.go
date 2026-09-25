package event

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* NewRequiredListenerSkippedError reports that a listener stopped propagation while a listener marked required through RequiredListenerRegistrar was still behind it. Its type lets a caller refuse the dispatch outright, as the http kernel does for kernel.request. Type-assert the error a dispatch returns directly rather than through errors.As: a nested dispatch's refusal travels up as the cause of an ordinary listener error. */
func NewRequiredListenerSkippedError(eventName string, stoppedByListenerName string) *RequiredListenerSkippedError {
    return &RequiredListenerSkippedError{
        exceptionErr: exception.NewError(
            "event propagation stopped before a required listener ran",
            exceptioncontract.Context{
                "eventName":         eventName,
                "stoppedByListener": stoppedByListenerName,
            },
            nil,
        ),
    }
}

/* NewRequiredListenerSkippedErrorWithStoppedListenerFailure reports the stop's refusal for a listener that failed while also stopping propagation with a required listener behind it. The refusal keeps the stop's message and context, and the listener's failure travels as the cause. */
func NewRequiredListenerSkippedErrorWithStoppedListenerFailure(eventName string, stoppedByListenerName string, cause error) *RequiredListenerSkippedError {
    return &RequiredListenerSkippedError{
        exceptionErr: exception.NewError(
            "event propagation stopped before a required listener ran",
            exceptioncontract.Context{
                "eventName":         eventName,
                "stoppedByListener": stoppedByListenerName,
            },
            cause,
        ),
    }
}

/* NewRequiredListenerSkippedErrorWithCause reports the same refusal for a dispatch that aborted on a failing listener with a required listener still behind it; the failure travels as the cause. */
func NewRequiredListenerSkippedErrorWithCause(eventName string, failedListenerName string, cause error) *RequiredListenerSkippedError {
    return &RequiredListenerSkippedError{
        exceptionErr: exception.NewError(
            "event dispatch failed before a required listener ran",
            exceptioncontract.Context{
                "eventName":      eventName,
                "failedListener": failedListenerName,
            },
            cause,
        ),
    }
}

type RequiredListenerSkippedError struct {
    exceptionErr *exception.Error
}

func (instance *RequiredListenerSkippedError) Error() string {
    /* the zero value is constructible outside the constructors, which always set the field */
    if nil == instance.exceptionErr {
        return "required listener skipped error carries no error value"
    }

    return instance.exceptionErr.Error()
}

func (instance *RequiredListenerSkippedError) Unwrap() error {
    /* returning the nil field through the interface would box a typed nil that passes every nil comparison downstream */
    if nil == instance.exceptionErr {
        return nil
    }

    return instance.exceptionErr
}
