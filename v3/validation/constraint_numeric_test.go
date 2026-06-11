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
