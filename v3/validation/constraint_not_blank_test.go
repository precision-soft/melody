package validation

import (
    "testing"
)

func TestNotBlank_NilPointerIsRejected(t *testing.T) {
    constraint := &NotBlank{}

    var nilPointer *string

    validationError := constraint.Validate(nilPointer, "field")

    if nil == validationError {
        t.Fatalf("fail-open: nil *string passed notBlank")
    }
}

func TestNotBlank_PointerToWhitespaceIsRejected(t *testing.T) {
    constraint := &NotBlank{}

    validationError := constraint.Validate(pointerOf("   "), "field")

    if nil == validationError {
        t.Fatalf("fail-open: *string whitespace passed notBlank (address rendered instead of value)")
    }
}
