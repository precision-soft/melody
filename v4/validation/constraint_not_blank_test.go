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

func TestNotBlank_NonStringIsRejected(t *testing.T) {
    constraint := &NotBlank{}

    for _, value := range []any{false, 0, []string{}, map[string]string{}} {
        validationError := constraint.Validate(value, "field")

        if nil == validationError {
            t.Fatalf("fail-open: %T passed notBlank through its rendering", value)
        }
    }
}
