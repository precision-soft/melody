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
