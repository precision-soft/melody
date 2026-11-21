package validation

import (
    "testing"

    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
)

func TestAlpha_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("abc123"))

    if nil == validationError {
        t.Fatalf("fail-open: non-alpha via *string passed validation")
    }
}

func TestAlpha_NonStringIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 12345)

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alpha constraint")
    }
}

func TestAlpha_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Alpha{}, "letters", ConstraintAlphaErrorNotAlpha)
}

func assertCharacterConstraintExits(t *testing.T, constraint interface {
    Validate(validationContext validationcontract.ValidationContext, value any) validationcontract.ValidationError
}, validValue string, expectedRefusalCode string) {
    t.Helper()

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nil); nil != validationError {
        t.Fatalf("expected an absent value to pass, got %v", validationError)
    }

    var absentPointer *string
    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, absentPointer); nil != validationError {
        t.Fatalf("expected a nil pointer to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, ""); nil != validationError {
        t.Fatalf("expected an empty string to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("")); nil != validationError {
        t.Fatalf("expected a pointer to an empty string to pass, got %v", validationError)
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, validValue); nil != validationError {
        t.Fatalf("expected %q to pass, got %v", validValue, validationError)
    }

    for _, refusedValue := range []any{42, []byte("letters"), true, 1.5, []string{"letters"}} {
        validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, refusedValue)
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
