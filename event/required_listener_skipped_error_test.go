package event

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* @info the TYPE is what this error exists for: the http kernel type-asserts it to refuse a kernel.request dispatch outright, because a listener that stopped propagation before a required one ran may still have produced a response — and serving that response means access control was never consulted. Nothing had ever built one, so neither the assertion the kernel makes nor the context an operator reads had been exercised. */
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

/* @info an ordinary listener failure must NOT assert to this type, or the kernel would refuse every dispatch that merely errored — the separation of the two classes is the whole point of declaring a type rather than a message. */
func TestRequiredListenerSkippedError_AnOrdinaryFailureDoesNotAssertToIt(t *testing.T) {
    ordinaryErr := exception.NewError("the listener refused", nil, nil)

    var typedError *RequiredListenerSkippedError
    if true == errors.As(error(ordinaryErr), &typedError) {
        t.Fatalf("expected an ordinary listener failure to stay out of this class")
    }
}
