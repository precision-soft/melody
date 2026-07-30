package validation

import (
    "testing"
)

func TestAlpha_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(pointerOf("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alpha via *string passed validation")
    }
}

/* @info the character rule can never hold in a non-string; the skip silently unenforced the rule */
func TestAlpha_NonStringIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(12345, "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alpha constraint")
    }
}
