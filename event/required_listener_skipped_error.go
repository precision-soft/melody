package event

import (
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

type RequiredListenerSkippedError struct {
    exceptionErr *exception.Error
}

func (instance *RequiredListenerSkippedError) Error() string {
    return instance.exceptionErr.Error()
}

func (instance *RequiredListenerSkippedError) Unwrap() error {
    return instance.exceptionErr
}
