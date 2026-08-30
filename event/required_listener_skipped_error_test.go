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

/* the zero value is constructible outside the constructors that always set the field, and the http kernel reaches this type by assertion and then renders it: dereferencing there replaced a refused request with a dead process. The sibling this type is shaped after answers both questions the same way. */
func TestRequiredListenerSkippedError_TheZeroValueAnswersInsteadOfDereferencing(t *testing.T) {
    zeroValue := &RequiredListenerSkippedError{}

    if "" == zeroValue.Error() {
        t.Fatalf("expected the zero value to answer with a message")
    }

    if nil != zeroValue.Unwrap() {
        t.Fatalf("expected the zero value to unwrap to a real nil, got %#v", zeroValue.Unwrap())
    }
}
