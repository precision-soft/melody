package validation

import (
    "testing"
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

/* @info a length constraint measures a string, not a Go rendering: an empty slice passed min=1 because its rendering [] is two runes long */
func TestMinLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMinLength(1)

    validationError := constraint.Validate([]string{}, "field")

    if nil == validationError {
        t.Fatalf("fail-open: empty slice passed minLength through its rendering")
    }
}

/* @info a length is never negative, so a negative bound is a typo that would make the rule a silent no-op */
func TestMinLength_WithParamsRefusesNegativeBound(t *testing.T) {
    constraint := NewMinLength(1)

    configured, withParamsErr := constraint.WithParams(map[string]string{"value": "-1"})

    if nil == withParamsErr || nil != configured {
        t.Fatalf("expected a negative min length bound to be refused")
    }
}
