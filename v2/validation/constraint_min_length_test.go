package validation

import (
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestMinLength_PointerToShortStringIsRejected(t *testing.T) {
    constraint := NewMinLength(5)

    validationError := constraint.Validate(pointerOf("ab"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: short *string passed minLength (address length measured instead of value)")
    }
}

func TestMinLength_PointerToValidStringPasses(t *testing.T) {
    constraint := NewMinLength(2)

    validationError := constraint.Validate(pointerOf("abcd"), "field")

    if nil != validationError {
        t.Fatalf("expected valid *string to pass minLength, got: %s", validationError.Error())
    }
}

/* a length constraint measures a string, not a Go rendering: an empty slice passed min=1 because its rendering [] is two runes long */
func TestMinLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMinLength(1)

    validationError := constraint.Validate([]string{}, "field")

    if nil == validationError {
        t.Fatalf("fail-open: empty slice passed minLength through its rendering")
    }
}

/* a length is never negative, so a negative bound is a typo that would make the rule a silent no-op */
func TestMinLength_WithParamsRefusesNegativeBound(t *testing.T) {
    constraint := NewMinLength(1)

    configured, withParamsErr := constraint.WithParams(map[string]string{"value": "-1"})

    if nil == withParamsErr || nil != configured {
        t.Fatalf("expected a negative min length bound to be refused")
    }
}

/* the constructor refuses what the tag door beside it has always refused: a negative minimum built a constraint that accepted every value in silence — a declaration that reads as enforced, validates nothing, and leaves no record anywhere that it does not */
func TestMinLength_TheConstructorRefusesANegativeBound(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewMinLength(-1)
        },
        "min length constraint may not be negative",
    )
}

/* zero is a bound and not a mistake: it is the one non-negative value that reads like one, and the sibling refusal must not swallow it */
func TestMinLength_ZeroIsABoundRatherThanAMistake(t *testing.T) {
    constraint := NewMinLength(0)

    if nil != constraint.Validate("", "field") {
        t.Fatalf("expected the empty string to satisfy a zero bound")
    }
}

/* the lower bound refuses a non-string for the same reason the upper one does, and its accessor was equally unentered: a min that read back as zero would report every field as unbounded to whoever asked. */
func TestMinLength_MeasuresStringsAndRefusesEverythingElse(t *testing.T) {
    constraint := NewMinLength(3)

    if 3 != constraint.Min() {
        t.Fatalf("expected the accessor to answer the configured bound, got %d", constraint.Min())
    }

    for _, refusedValue := range []any{42, []byte("abc"), []string{"a", "b", "c"}, true} {
        validationError := constraint.Validate(refusedValue, "field")
        if nil == validationError {
            t.Fatalf("expected %#v to be refused rather than measured, got a pass", refusedValue)
        }

        if "value must be a string" != validationError.Message() {
            t.Fatalf("expected the type refusal for %#v, got %q", refusedValue, validationError.Message())
        }
    }

    if validationError := constraint.Validate("abc", "field"); nil != validationError {
        t.Fatalf("expected a string exactly at the bound to pass, got %v", validationError)
    }
}
