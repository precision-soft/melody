package validation

import (
    "testing"
)

func pointerOf(value string) *string {
    return &value
}

func TestEmail_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(pointerOf("definitely not an email"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via *string passed validation")
    }
}

func TestEmail_PointerToStringValidPasses(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(pointerOf("user@example.com"), "field")

    if nil != validationError {
        t.Fatalf("expected valid *string email to pass, got: %s", validationError.Error())
    }
}

func TestNumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Numeric{}

    validationError := constraint.Validate(pointerOf("12ab"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-numeric via *string passed validation")
    }
}

func TestAlpha_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(pointerOf("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alpha via *string passed validation")
    }
}

func TestAlphanumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate(pointerOf("with space"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alphanumeric via *string passed validation")
    }
}

func TestRegex_PointerToStringMismatchIsRejected(t *testing.T) {
    constraint := NewRegex(`^[0-9]{3}$`)

    validationError := constraint.Validate(pointerOf("abcd"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: regex mismatch via *string passed validation")
    }
}

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

func TestMinLength_PointerToShortStringIsRejected(t *testing.T) {
    constraint := NewMinLength(5)

    validationError := constraint.Validate(pointerOf("ab"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: short *string passed minLength (address length measured instead of value)")
    }
}

func TestMaxLength_PointerToLongStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(4)

    validationError := constraint.Validate(pointerOf("way too long for the limit"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: overlong *string passed maxLength (address length measured instead of value)")
    }
}

func TestMinLength_PointerToValidStringPasses(t *testing.T) {
    constraint := NewMinLength(2)

    validationError := constraint.Validate(pointerOf("abcd"), "field")

    if nil != validationError {
        t.Fatalf("expected valid *string to pass minLength, got: %s", validationError.Error())
    }
}
