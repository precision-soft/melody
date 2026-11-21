package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "testing"
)

func TestNumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Numeric{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("12ab"))

    if nil == validationError {
        t.Fatalf("fail-open: non-numeric via *string passed validation")
    }
}

func TestNumeric_NonStringIsRejected(t *testing.T) {
    constraint := &Numeric{}

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 12345)

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the numeric constraint")
    }
}

func TestNumeric_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Numeric{}, "123", ConstraintNumericErrorNotNumeric)
}
