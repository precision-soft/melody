package validation

import (
	"testing"
)

func TestGreaterThan_IntAboveMin_ReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(10, "age")
	if nil != validationError {
		t.Fatalf("expected nil error for int above min")
	}
}

func TestGreaterThan_IntEqualToMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5, "age")
	if nil == validationError {
		t.Fatalf("expected error when int equals min")
	}

	if ConstraintGreaterThanErrorSmallerThan != validationError.Code() {
		t.Fatalf("unexpected error code: %s", validationError.Code())
	}
}

func TestGreaterThan_IntBelowMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(3, "age")
	if nil == validationError {
		t.Fatalf("expected error when int is below min")
	}
}

func TestGreaterThan_NilValue_ReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(0)

	validationError := constraint.Validate(nil, "age")
	if nil != validationError {
		t.Fatalf("expected nil error for nil value")
	}
}

func TestGreaterThan_Float64AboveMin_ReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5.5, "price")
	if nil != validationError {
		t.Fatalf("expected nil error for float64 above min")
	}
}

func TestGreaterThan_Float64EqualToMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(5.0, "price")
	if nil == validationError {
		t.Fatalf("expected error when float64 equals min")
	}
}

func TestGreaterThan_Float64BelowMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(4.9, "price")
	if nil == validationError {
		t.Fatalf("expected error when float64 is below min")
	}
}

func TestGreaterThan_Float32AboveMin_ReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(0)

	validationError := constraint.Validate(float32(1.5), "amount")
	if nil != validationError {
		t.Fatalf("expected nil error for float32 above min")
	}
}

func TestGreaterThan_UintAboveMin_ReturnsNil(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(uint(10), "count")
	if nil != validationError {
		t.Fatalf("expected nil error for uint above min")
	}
}

func TestGreaterThan_UintEqualToMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(uint(5), "count")
	if nil == validationError {
		t.Fatalf("expected error when uint equals min")
	}
}

func TestGreaterThan_UintBelowMin_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	validationError := constraint.Validate(uint(3), "count")
	if nil == validationError {
		t.Fatalf("expected error when uint is below min")
	}
}

func TestGreaterThan_UintWithNegativeMin_AlwaysPasses(t *testing.T) {
	constraint := NewGreaterThan(-1)

	validationError := constraint.Validate(uint(0), "count")
	if nil != validationError {
		t.Fatalf("expected nil error for uint with negative min")
	}
}

func TestGreaterThan_IntPointer_UnwrapsPointer(t *testing.T) {
	constraint := NewGreaterThan(5)

	value := 10
	validationError := constraint.Validate(&value, "age")
	if nil != validationError {
		t.Fatalf("expected nil error for pointer to int above min")
	}
}

func TestGreaterThan_NilPointer_ReturnsError(t *testing.T) {
	constraint := NewGreaterThan(5)

	var value *int
	validationError := constraint.Validate(value, "age")
	if nil == validationError {
		t.Fatalf("expected error for nil pointer")
	}
}

func TestGreaterThan_StringValue_ReturnsTypeError(t *testing.T) {
	constraint := NewGreaterThan(0)

	validationError := constraint.Validate("hello", "name")
	if nil == validationError {
		t.Fatalf("expected error for unsupported string type")
	}

	if "value must be an integer" != validationError.Message() {
		t.Fatalf("unexpected message: %s", validationError.Message())
	}
}

func TestGreaterThan_MinAccessor_ReturnsConfiguredMin(t *testing.T) {
	constraint := NewGreaterThan(42)

	if 42 != constraint.Min() {
		t.Fatalf("expected min to be 42, got %d", constraint.Min())
	}
}

type greaterThanPayload struct {
	Age   int     `json:"age" validate:"greaterThan=18"`
	Price float64 `json:"price" validate:"greaterThan=0"`
}

func TestValidator_GreaterThan_AcceptsValidIntField(t *testing.T) {
	validatorInstance := NewValidator()

	payload := greaterThanPayload{
		Age:   25,
		Price: 9.99,
	}

	err := validatorInstance.Validate(payload)
	requireNoValidationErrors(t, err)
}

func TestValidator_GreaterThan_RejectsInvalidIntField(t *testing.T) {
	validatorInstance := NewValidator()

	payload := greaterThanPayload{
		Age:   10,
		Price: 9.99,
	}

	err := validatorInstance.Validate(payload)
	_ = requireValidationErrors(t, err)
}

func TestValidator_GreaterThan_RejectsZeroFloat(t *testing.T) {
	validatorInstance := NewValidator()

	payload := greaterThanPayload{
		Age:   25,
		Price: 0.0,
	}

	err := validatorInstance.Validate(payload)
	_ = requireValidationErrors(t, err)
}
