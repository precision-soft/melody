package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "testing"
)

func TestEmail_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("definitely not an email"))

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via *string passed validation")
    }
}

func TestEmail_NamedStringTypeInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, namedString("definitely not an email"))

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via named string type passed validation")
    }
}

func TestEmail_PointerToStringValidPasses(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("user@example.com"))

    if nil != validationError {
        t.Fatalf("expected valid *string email to pass, got: %s", validationError.Error())
    }
}

func TestEmail_NonStringIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 12345)

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the email constraint")
    }
}

func TestEmail_AnAbsentValueIsOptionalityRatherThanAMalformedAddress(t *testing.T) {
    constraint := &Email{}

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nil); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, absentPointer); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("user@example.com")); nil != validationError {
        t.Fatalf("expected a valid address to pass, got %v", validationError)
    }
}
