package event

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* NewRequiredListenerSkippedError reports propagation stopping before a required listener. Type-assert the direct dispatch error to classify this dispatch; errors.As can also find a nested dispatch’s refusal, which expresses a broader policy. */
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

/* NewRequiredListenerSkippedErrorWithStoppedListenerFailure retains the propagation-stop diagnostic and includes the failing stopped listener’s error as its cause. */
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

/* NewRequiredListenerSkippedErrorWithCause reports a required listener skipped because an earlier listener failed, retaining that failure as its cause. */
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

    if nil == instance.exceptionErr {
        return "required listener skipped error carries no error value"
    }

    return instance.exceptionErr.Error()
}

func (instance *RequiredListenerSkippedError) Unwrap() error {

    if nil == instance.exceptionErr {
        return nil
    }

    return instance.exceptionErr
}
