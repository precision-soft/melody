package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "testing"
)

func TestAlphanumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("with space"))

    if nil == validationError {
        t.Fatalf("fail-open: non-alphanumeric via *string passed validation")
    }
}

func TestAlphanumeric_NonStringIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, []byte("abc123"))

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alphanumeric constraint")
    }
}

func TestAlphanumeric_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Alphanumeric{}, "letters123", ConstraintAlphanumericErrorNotAlphanumeric)
}
