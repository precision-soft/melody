package validation

import (
    "math"
    "testing"
)

func TestLessThan_IntegerPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(5, "field")

    if nil != validationError {
        t.Fatalf("expected no error, got: %s", validationError.Error())
    }
}

func TestLessThan_IntegerFailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(10, "field")

    if nil == validationError {
        t.Fatalf("expected error when value equals max")
    }

    if ConstraintLessThanErrorGreaterThan != validationError.Code() {
        t.Fatalf("expected code %s, got: %s", ConstraintLessThanErrorGreaterThan, validationError.Code())
    }
}

func TestLessThan_IntegerFailsWhenAboveMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(15, "field")

    if nil == validationError {
        t.Fatalf("expected error when value is above max")
    }
}

func TestLessThan_Float64PassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(9.5, "field")

    if nil != validationError {
        t.Fatalf("expected no error for float below max, got: %s", validationError.Error())
    }
}

func TestLessThan_Float64FailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(10.0, "field")

    if nil == validationError {
        t.Fatalf("expected error when float equals max")
    }
}

func TestLessThan_UintPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(uint(5), "field")

    if nil != validationError {
        t.Fatalf("expected no error for uint below max, got: %s", validationError.Error())
    }
}

func TestLessThan_UintFailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(uint(10), "field")

    if nil == validationError {
        t.Fatalf("expected error when uint equals max")
    }
}

func TestLessThan_UintAlwaysFailsWhenMaxIsNegative(t *testing.T) {
    constraint := NewLessThan(-1)

    validationError := constraint.Validate(uint(0), "field")

    if nil == validationError {
        t.Fatalf("expected error for uint when max is negative")
    }
}

func TestLessThan_NilValueReturnsNil(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(nil, "field")

    if nil != validationError {
        t.Fatalf("expected nil for nil value")
    }
}

func TestLessThan_StringValueReturnsError(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate("not a number", "field")

    if nil == validationError {
        t.Fatalf("expected error for non-numeric value")
    }
}

func TestLessThan_NilPointerReturnsError(t *testing.T) {
    constraint := NewLessThan(10)

    var nilPointer *int

    validationError := constraint.Validate(nilPointer, "field")

    if nil == validationError {
        t.Fatalf("expected error for nil pointer")
    }
}

func TestLessThan_PointerToIntPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    value := 5
    validationError := constraint.Validate(&value, "field")

    if nil != validationError {
        t.Fatalf("expected no error for pointer to int below max, got: %s", validationError.Error())
    }
}

func TestLessThan_MaxGetter(t *testing.T) {
    constraint := NewLessThan(42)

    if 42 != constraint.Max() {
        t.Fatalf("expected Max() to return 42, got: %d", constraint.Max())
    }
}

func TestLessThan_ErrorCodeDoesNotCollideWithGreaterThanConstraintName(t *testing.T) {
    if ConstraintGreaterThan == ConstraintLessThanErrorGreaterThan {
        t.Fatalf("LessThan error code %q collides with the GreaterThan constraint tag name — they must be distinct strings", ConstraintLessThanErrorGreaterThan)
    }
}

func TestLessThan_RejectsNaN(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(math.NaN(), "field")

    if nil == validationError {
        t.Fatalf("expected NaN to be rejected by lessThan, but it passed validation")
    }
}
