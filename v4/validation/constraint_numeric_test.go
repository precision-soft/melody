package validation

import (
    "testing"
)

func TestNumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Numeric{}

    validationError := constraint.Validate(pointerOf("12ab"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-numeric via *string passed validation")
    }
}

func TestNumeric_NonStringIsRejected(t *testing.T) {
    constraint := &Numeric{}

    validationError := constraint.Validate(12345, "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the numeric constraint")
    }
}

func TestNumeric_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Numeric{}, "123", ConstraintNumericErrorNotNumeric)
}
