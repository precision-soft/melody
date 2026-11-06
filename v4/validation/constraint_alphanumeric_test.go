package validation

import (
    "testing"
)

func TestAlphanumeric_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate(pointerOf("with space"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alphanumeric via *string passed validation")
    }
}

func TestAlphanumeric_NonStringIsRejected(t *testing.T) {
    constraint := &Alphanumeric{}

    validationError := constraint.Validate([]byte("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-string passed the alphanumeric constraint")
    }
}

func TestAlphanumeric_TheFourExitsTakenBeforeThePatternRuns(t *testing.T) {
    assertCharacterConstraintExits(t, &Alphanumeric{}, "letters123", ConstraintAlphanumericErrorNotAlphanumeric)
}
