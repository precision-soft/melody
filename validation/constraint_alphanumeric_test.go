package validation

import (
    "testing"
)

func TestAlphanumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate(pointerOf("with space"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alphanumeric via *string passed validation")
    }
}

/* @info the character rule can never hold in a non-string; the skip silently unenforced the rule */
func TestAlphanumeric_NonStringIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate([]byte("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alphanumeric constraint")
    }
}
