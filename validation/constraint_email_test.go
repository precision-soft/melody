package validation

import (
    "testing"
)

func TestEmail_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(pointerOf("definitely not an email"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via *string passed validation")
    }
}

func TestEmail_NamedStringTypeInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(namedString("definitely not an email"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via named string type passed validation")
    }
}

func TestEmail_PointerToStringValidPasses(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(pointerOf("user@example.com"), "field")

    if nil != validationError {
        t.Fatalf("expected valid *string email to pass, got: %s", validationError.Error())
    }
}

/* @info an email format can never hold in a non-string; the skip silently unenforced the rule */
func TestEmail_NonStringIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(12345, "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the email constraint")
    }
}
