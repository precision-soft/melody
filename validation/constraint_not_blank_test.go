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

/* @info blankness is a property of a string: the fmt rendering of anything else is never blank, so false, 0, an empty slice and an empty map all passed a rule the developer wrote to require presence */
func TestNotBlank_NonStringIsRejected(t *testing.T) {
    constraint := &NotBlank{}

    for _, value := range []any{false, 0, []string{}, map[string]string{}} {
        validationError := constraint.Validate(value, "field")

        if nil == validationError {
            t.Fatalf("fail-open: %T passed notBlank through its rendering", value)
        }
    }
}
