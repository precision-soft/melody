package validation

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestMaxLength_PointerToLongStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(4)

    validationError := constraint.Validate(pointerOf("way too long for the limit"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: overlong *string passed maxLength (address length measured instead of value)")
    }
}

/* a length constraint measures a string, not a Go rendering: an int whose single-digit rendering sat under the bound passed a rule that cannot measure it — the short rendering is what tells the type guard apart from the rendering it replaced */
func TestMaxLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(5)

    validationError := constraint.Validate(7, "field")

    if nil == validationError {
        t.Fatalf("fail-open: an int passed maxLength through its rendering")
    }
}

/* a length is never negative, so a negative bound is a typo that would reject every value with a message naming an impossible limit */
func TestMaxLength_WithParamsRefusesNegativeBound(t *testing.T) {
    constraint := NewMaxLength(5)

    configured, withParamsErr := constraint.WithParams(map[string]string{"value": "-1"})

    if nil == withParamsErr || nil != configured {
        t.Fatalf("expected a negative max length bound to be refused")
    }
}

/* the constructor refuses what the tag door beside it has always refused: a negative maximum built a constraint that answered "this field must not exceed -1 characters" to every value, the empty string included, and that message reached the client. The two doors of one constraint used to judge the same typo differently. */
func TestMaxLength_TheConstructorRefusesANegativeBound(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewMaxLength(-1)
        },
        "max length constraint may not be negative",
    )
}

/* zero is a bound and not a mistake: it is the one non-negative value that reads like one, and the sibling refusal must not swallow it — a rule declaring that a field carries no characters is a rule */
func TestMaxLength_ZeroIsABoundRatherThanAMistake(t *testing.T) {
    constraint := NewMaxLength(0)

    if nil != constraint.Validate("", "field") {
        t.Fatalf("expected the empty string to satisfy a zero bound")
    }

    if nil == constraint.Validate("a", "field") {
        t.Fatalf("expected a single character to exceed a zero bound")
    }
}

/* a length constraint measures a string, and anything else is a declaration mistake refused rather than measured: the value used to be fmt-formatted first, so an empty slice passed max=1 on the strength of its two-character rendering while the payload it stood for did not. The accessor beside it had never been executed by anything — it is how a caller reads back the bound a parsed tag produced, so one that answered the wrong field would misreport every limit in an introspection or an error message. */
func TestMaxLength_MeasuresStringsAndRefusesEverythingElse(t *testing.T) {
    constraint := NewMaxLength(4)

    if 4 != constraint.Max() {
        t.Fatalf("expected the accessor to answer the configured bound, got %d", constraint.Max())
    }

    for _, refusedValue := range []any{42, []byte("ab"), []string{}, map[string]string{}, true, 1.5} {
        validationError := constraint.Validate(refusedValue, "field")
        if nil == validationError {
            t.Fatalf("expected %#v to be refused rather than measured, got a pass", refusedValue)
        }

        if "value must be a string" != validationError.Message() {
            t.Fatalf("expected the type refusal for %#v, got %q", refusedValue, validationError.Message())
        }
    }

    if validationError := constraint.Validate("abcd", "field"); nil != validationError {
        t.Fatalf("expected a string exactly at the bound to pass, got %v", validationError)
    }
}

/* a length constraint treats an absent value as optionality rather than as a zero-length string. The two answers differ exactly where it matters: a nil pointer under min=3 must pass — nothing was supplied — while an empty string under it must fail. */
func TestMaxLength_AnAbsentValueIsOptionalityRatherThanAZeroLengthString(t *testing.T) {
    if validationError := NewMaxLength(4).Validate(nil, "field"); nil != validationError {
        t.Fatalf("expected an absent value to pass the upper bound, got %v", validationError)
    }

    var absentPointer *string
    if validationError := NewMaxLength(4).Validate(absentPointer, "field"); nil != validationError {
        t.Fatalf("expected a nil pointer to pass the upper bound, got %v", validationError)
    }

    if validationError := NewMinLength(3).Validate(absentPointer, "field"); nil != validationError {
        t.Fatalf("expected a nil pointer to pass the lower bound, got %v", validationError)
    }

    if validationError := NewMinLength(3).Validate("", "field"); nil == validationError {
        t.Fatalf("expected an empty string to fail the lower bound, which an absent value passes")
    }
}
