package validation

import (
    "math"
    "testing"
)

func TestLessThan_RejectsNaN(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(math.NaN(), "field")

    if nil == validationError {
        t.Fatalf("expected NaN to be rejected by lessThan, but it passed validation")
    }
}

func TestGreaterThan_RejectsNaN(t *testing.T) {
    constraint := NewGreaterThan(10)

    validationError := constraint.Validate(math.NaN(), "field")

    if nil == validationError {
        t.Fatalf("expected NaN to be rejected by greaterThan, but it passed validation")
    }
}
