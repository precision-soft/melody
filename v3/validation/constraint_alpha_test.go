package validation

import (
    "testing"

    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

func TestAlpha_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(pointerOf("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alpha via *string passed validation")
    }
}

func TestAlpha_NonStringIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(12345, "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alpha constraint")
    }
}

func TestAlpha_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Alpha{}, "letters", ConstraintAlphaErrorNotAlpha)
}

func assertCharacterConstraintExits(t *testing.T, constraint interface {
    Validate(value any, field string) validationcontract.ValidationError
}, validValue string, expectedRefusalCode string) {
    t.Helper()

    if validationError := constraint.Validate(nil, "field"); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(absentPointer, "field"); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }

    if validationError := constraint.Validate("", "field"); nil != validationError {
        t.Fatalf("expected an empty string to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(pointerOf(""), "field"); nil != validationError {
        t.Fatalf("expected a pointer to an empty string to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(validValue, "field"); nil != validationError {
        t.Fatalf("expected %q to pass, got %v", validValue, validationError)
    }

    for _, refusedValue := range []any{42, []byte("letters"), true, 1.5, []string{"letters"}} {
        validationError := constraint.Validate(refusedValue, "field")
        if nil == validationError {
            t.Fatalf("expected %#v to be refused rather than silently passed", refusedValue)
        }

        if "value must be a string" != validationError.Message() {
            t.Fatalf("expected the type refusal message for %#v, got %q", refusedValue, validationError.Message())
        }

        if expectedRefusalCode != validationError.Code() {
            t.Fatalf("expected the refusal to carry the constraint's own code, got %q", validationError.Code())
        }
    }
}
