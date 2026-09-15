package validation

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
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

func TestMinLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMinLength(1)

    validationError := constraint.Validate([]string{}, "field")

    if nil == validationError {
        t.Fatalf("fail-open: empty slice passed minLength through its rendering")
    }
}

func TestMinLength_WithParamsRefusesNegativeBound(t *testing.T) {
    constraint := NewMinLength(1)

    configured, withParamsErr := constraint.WithParams(map[string]string{"value": "-1"})

    if nil == withParamsErr || nil != configured {
        t.Fatalf("expected a negative min length bound to be refused")
    }
}

func TestMinLength_TheConstructorRefusesANegativeBound(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewMinLength(-1)
        },
        "min length constraint may not be negative",
    )
}

func TestMinLength_ZeroIsABoundRatherThanAMistake(t *testing.T) {
    constraint := NewMinLength(0)

    if nil != constraint.Validate("", "field") {
        t.Fatalf("expected the empty string to satisfy a zero bound")
    }
}

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
