package event

import (
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* NewRequiredListenerSkippedError reports that a listener stopped event propagation while a listener marked required through RequiredListenerRegistrar was still behind it, so that listener never ran. The type matters more than the message: it is what lets a caller separate this class from an ordinary listener failure and refuse the dispatch outright, which is what the http kernel does for kernel.request — a stopping listener that also produced a response would otherwise have that response served with access control never consulted.

Type-assert the error a dispatch returns directly rather than reaching through the cause chain with errors.As: a listener is free to dispatch further events, and one of those nested dispatches skipping a required listener of its own travels up as the cause of an ordinary listener error. Failing the outer dispatch closed for that is a different, wider policy than refusing the dispatch that actually skipped the listener. */
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

/* NewRequiredListenerSkippedErrorWithStoppedListenerFailure reports the stop's own refusal for a listener that FAILED while also stopping propagation with a required listener behind it. The refusal keeps the stop's message and context — the stop is the decision the caller reacts to — and the listener's failure travels as the cause: the failure was deliberately returned unlogged by the dispatch on the promise that the caller's record names it, and without the cause this was the one path on which it reached no log at all. */
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

/* NewRequiredListenerSkippedErrorWithCause reports the same refusal for a dispatch that ABORTED on a failing listener while a listener marked required was still behind it: a listener that fails ends the dispatch exactly as decisively as one that stops propagation, so the required listener never ran. The failure that ended the dispatch travels as the cause, so the diagnostic of the listener that actually broke is not lost behind the refusal. */
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
    return instance.exceptionErr.Error()
}

func (instance *RequiredListenerSkippedError) Unwrap() error {
    return instance.exceptionErr
}
