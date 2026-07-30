package validation

import (
    "testing"
)

func TestMaxLength_PointerToLongStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(4)

    validationError := constraint.Validate(pointerOf("way too long for the limit"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: overlong *string passed maxLength (address length measured instead of value)")
    }
}

/* @info a length constraint measures a string, not a Go rendering: an int whose single-digit rendering sat under the bound passed a rule that cannot measure it — the short rendering is what tells the type guard apart from the rendering it replaced */
func TestMaxLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(5)

    validationError := constraint.Validate(7, "field")

    if nil == validationError {
        t.Fatalf("fail-open: an int passed maxLength through its rendering")
    }
}

/* @info a length is never negative, so a negative bound is a typo that would reject every value with a message naming an impossible limit */
func TestMaxLength_WithParamsRefusesNegativeBound(t *testing.T) {
    constraint := NewMaxLength(5)

    configured, withParamsErr := constraint.WithParams(map[string]string{"value": "-1"})

    if nil == withParamsErr || nil != configured {
        t.Fatalf("expected a negative max length bound to be refused")
    }
}
