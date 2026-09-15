package http

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestRecoverToError_ANilRecoveryIsNotAFailure(t *testing.T) {
    if nil != RecoverToError(nil) {
        t.Fatalf("expected a nil recovery to yield no error")
    }
}

func TestRecoverToError_ARecoveredErrorTravelsUnchanged(t *testing.T) {
    original := exception.NewError("the handler refused", map[string]any{"serviceName": "app.thing"}, nil)

    recovered := RecoverToError(original)

    if error(original) != recovered {
        t.Fatalf("expected the recovered error to travel unchanged, got %#v", recovered)
    }

    plain := errors.New("a plain failure")
    if plain != RecoverToError(plain) {
        t.Fatalf("expected a plain error to travel unchanged too")
    }
}

func TestRecoverToError_AStringPanicBecomesTheMessage(t *testing.T) {
    recovered := RecoverToError("the handler gave up")

    if nil == recovered {
        t.Fatalf("expected a string panic to yield an error")
    }

    if "the handler gave up" != recovered.Error() {
        t.Fatalf("unexpected message: %q", recovered.Error())
    }
}

func TestRecoverToError_AnyOtherValueIsRenderedIntoTheContext(t *testing.T) {
    recovered := RecoverToError(42)

    if nil == recovered {
        t.Fatalf("expected a non-error non-string panic to yield an error")
    }

    if "panic recovered" != recovered.Error() {
        t.Fatalf("unexpected message: %q", recovered.Error())
    }

    var typedError *exception.Error
    if false == errors.As(recovered, &typedError) {
        t.Fatalf("expected a melody error, got %T", recovered)
    }

    renderedValue, exists := typedError.Context()["value"].(string)
    if false == exists || false == strings.Contains(renderedValue, "42") {
        t.Fatalf("expected the panic value to be rendered into the context, got %#v", typedError.Context()["value"])
    }
}

func TestRecoverToError_NormalizesATypedNilErrorToTheGenericBranch(t *testing.T) {
    var typedNil *exception.Error

    recovered := RecoverToError(typedNil)
    if nil == recovered {
        t.Fatal("expected the typed nil panic value to still answer an error")
    }

    defer func() {
        if recoveredValue := recover(); nil != recoveredValue {
            t.Fatalf("expected the normalized error's Error() to be safe, got panic %v", recoveredValue)
        }
    }()

    if "" == recovered.Error() {
        t.Fatal("expected the normalized error to carry a message")
    }
}

func TestDebugErrorMessage_ContainsAPanickingError(t *testing.T) {
    message := debugErrorMessage(&panickingMessageError{})

    if false == strings.Contains(message, "error message panicked") {
        t.Fatalf("expected the contained panic note, got %q", message)
    }
}

type panickingMessageError struct{}

func (instance *panickingMessageError) Error() string {
    var m map[string]string
    m["boom"] = "boom"

    return "unreachable"
}
