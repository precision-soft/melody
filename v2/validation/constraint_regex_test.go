package validation

import (
    "testing"
)

func TestRegex_PointerToStringMismatchIsRejected(t *testing.T) {
    constraint := NewRegex(`^[0-9]{3}$`)

    validationError := constraint.Validate(pointerOf("abcd"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: regex mismatch via *string passed validation")
    }
}
