package validation

import (
    "testing"
)

func TestAlpha_PointerToStringInvalidIsRejected(t *testing.T) {
    constraint := &Alpha{}

    validationError := constraint.Validate(pointerOf("abc123"), "field")

    if nil == validationError {
        t.Fatalf("fail-open: non-alpha via *string passed validation")
    }
}
