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

func TestEmail_NonStringIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(12345, "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the email constraint")
    }
}

func TestEmail_AnAbsentValueIsOptionalityRatherThanAMalformedAddress(t *testing.T) {
    constraint := &Email{}

    if validationError := constraint.Validate(nil, "field"); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(absentPointer, "field"); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(pointerOf("user@example.com"), "field"); nil != validationError {
        t.Fatalf("expected a valid address to pass, got %v", validationError)
    }
}
