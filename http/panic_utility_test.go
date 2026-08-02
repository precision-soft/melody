package http

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* @info every recovery boundary of the request path funnels its recovered value through here, and nothing had ever called it: a nil that did not read as nil would turn a clean return into a fabricated failure, and a value dropped instead of described would leave an operator with a 500 and nothing to look at. A recovered error travels UNCHANGED — wrapping it would bury the level, the context and the already-logged mark the exception package carries. */
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

/* @info a string panic — the shape a hand-written panic("...") takes — becomes the message itself rather than being buried under a generic one, because that string is everything the author of the panic said. */
func TestRecoverToError_AStringPanicBecomesTheMessage(t *testing.T) {
    recovered := RecoverToError("the handler gave up")

    if nil == recovered {
        t.Fatalf("expected a string panic to yield an error")
    }

    if "the handler gave up" != recovered.Error() {
        t.Fatalf("unexpected message: %q", recovered.Error())
    }
}

/* @info anything else — an int, a struct, a nil map dereference value — is rendered into the context under a message that says what happened, so the report names the value instead of dropping it. */
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
