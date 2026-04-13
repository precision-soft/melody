package validation

import (
	"testing"
)

func TestGreaterThan_IntegerPassesWhenAboveMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(10, "field")

	if nil != validationError {
		t.Fatalf("expected no error, got: %s", validationError.Error())
	}
}

func TestGreaterThan_IntegerFailsWhenEqualToMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5, "field")

	if nil == validationError {
		t.Fatalf("expected error when value equals min")
	}

	if ConstraintGreaterThanErrorSmallerThan != validationError.Code() {
		t.Fatalf("expected code %s, got: %s", ConstraintGreaterThanErrorSmallerThan, validationError.Code())
	}
}

func TestGreaterThan_IntegerFailsWhenBelowMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(3, "field")

	if nil == validationError {
		t.Fatalf("expected error when value is below min")
	}
}

func TestGreaterThan_Float64PassesWhenAboveMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5.5, "field")

	if nil != validationError {
		t.Fatalf("expected no error for float above min, got: %s", validationError.Error())
	}
}

func TestGreaterThan_Float64FailsWhenEqualToMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5.0, "field")

	if nil == validationError {
		t.Fatalf("expected error when float equals min")
	}
}

func TestGreaterThan_Float64FailsWhenBelowMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(4.9, "field")

	if nil == validationError {
		t.Fatalf("expected error when float is below min")
	}
}

func TestGreaterThan_Float32PassesWhenAboveMin(t *testing.T) {
	constraint := NewGreaterThan(0)

	validationError := constraint.Validate(float32(0.5), "field")

	if nil != validationError {
		t.Fatalf("expected no error for float32 above min, got: %s", validationError.Error())
	}
}

func TestGreaterThan_UintPassesWhenAboveMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(uint(10), "field")

	if nil != validationError {
		t.Fatalf("expected no error for uint above min, got: %s", validationError.Error())
	}
}

func TestGreaterThan_UintFailsWhenEqualToMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(uint(5), "field")

	if nil == validationError {
		t.Fatalf("expected error when uint equals min")
	}
}

func TestGreaterThan_UintAlwaysPassesWhenMinIsNegative(t *testing.T) {
	constraint := NewGreaterThan(-1)

	validationError := constraint.Validate(uint(0), "field")

	if nil != validationError {
		t.Fatalf("expected no error for uint when min is negative, got: %s", validationError.Error())
	}
}

func TestGreaterThan_NilValueReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(nil, "field")

	if nil != validationError {
		t.Fatalf("expected nil for nil value")
	}
}

func TestGreaterThan_StringValueReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate("not a number", "field")

	if nil == validationError {
		t.Fatalf("expected error for non-numeric value")
	}
}

func TestGreaterThan_NilPointerReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	var nilPointer *int

	validationError := constraint.Validate(nilPointer, "field")

	if nil == validationError {
		t.Fatalf("expected error for nil pointer")
	}
}

func TestGreaterThan_PointerToIntPassesWhenAboveMin(t *testing.T) {
	constraint := NewGreaterThan(5)

	value := 10
	validationError := constraint.Validate(&value, "field")

	if nil != validationError {
		t.Fatalf("expected no error for pointer to int above min, got: %s", validationError.Error())
	}
}

func TestGreaterThan_MinGetter(t *testing.T) {
	constraint := NewGreaterThan(42)

	if 42 != constraint.Min() {
		t.Fatalf("expected Min() to return 42, got: %d", constraint.Min())
	}
}
