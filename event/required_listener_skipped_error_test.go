package event

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/exception"
)

func TestNewRequiredListenerSkippedError_IsAssertableAndNamesWhatWasSkipped(t *testing.T) {
    skippedErr := NewRequiredListenerSkippedError("kernel.request", "app.short.circuit.listener")

    var typedError *RequiredListenerSkippedError
    if false == errors.As(error(skippedErr), &typedError) {
        t.Fatalf("expected the error to be assertable to its own type")
    }

    if "event propagation stopped before a required listener ran" != skippedErr.Error() {
        t.Fatalf("unexpected message: %q", skippedErr.Error())
    }

    var wrappedError *exception.Error
    if false == errors.As(skippedErr.Unwrap(), &wrappedError) {
        t.Fatalf("expected the wrapped melody error to be reachable")
    }

    if "kernel.request" != wrappedError.Context()["eventName"] {
        t.Fatalf("expected the event to be named, got %#v", wrappedError.Context()["eventName"])
    }

    if "app.short.circuit.listener" != wrappedError.Context()["stoppedByListener"] {
        t.Fatalf("expected the listener that stopped propagation to be named, got %#v", wrappedError.Context()["stoppedByListener"])
    }
}

func TestRequiredListenerSkippedError_AnOrdinaryFailureDoesNotAssertToIt(t *testing.T) {
    ordinaryErr := exception.NewError("the listener refused", nil, nil)

    var typedError *RequiredListenerSkippedError
    if true == errors.As(error(ordinaryErr), &typedError) {
        t.Fatalf("expected an ordinary listener failure to stay out of this class")
    }
}
