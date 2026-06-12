package validation

import (
    "testing"
)

type namedString string

func TestDereferenceValue_NamedStringNormalizesToPlainString(t *testing.T) {
    resolved, ok := dereferenceValue(namedString("user@example.com"))
    if false == ok {
        t.Fatalf("expected named string to resolve")
    }

    if _, isString := resolved.(string); false == isString {
        t.Fatalf("expected named string to normalize to plain string, got %T", resolved)
    }
}

func TestEmail_NamedStringTypeInvalidIsRejected(t *testing.T) {
    constraint := &Email{}

    validationError := constraint.Validate(namedString("definitely not an email"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: invalid email via named string type passed validation")
    }
}
