package http

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
)

/* every recovery boundary of the request path funnels its recovered value through here, and nothing had ever called it: a nil that did not read as nil would turn a clean return into a fabricated failure, and a value dropped instead of described would leave an operator with a 500 and nothing to look at. A recovered error travels UNCHANGED — wrapping it would bury the level, the context and the already-logged mark the exception package carries. */
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

/* a string panic — the shape a hand-written panic("...") takes — becomes the message itself rather than being buried under a generic one, because that string is everything the author of the panic said. */
func TestRecoverToError_AStringPanicBecomesTheMessage(t *testing.T) {
    recovered := RecoverToError("the handler gave up")

    if nil == recovered {
        t.Fatalf("expected a string panic to yield an error")
    }

    if "the handler gave up" != recovered.Error() {
        t.Fatalf("unexpected message: %q", recovered.Error())
    }
}

/* anything else — an int, a struct, a nil map dereference value — is rendered into the context under a message that says what happened, so the report names the value instead of dropping it. */
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

/* the typed nil is normalized to the generic branch the way the exit handler's resolver normalizes it: passed through, the first Error() reader without its own guard — the kernel's debug-mode message, inside the recovery defer — raised a second panic that escaped ServeHttp. */
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

/* debugErrorMessage runs inside the recovery defer, where a message that panics would cost the connection: the panic is contained into a rendered note. */
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
