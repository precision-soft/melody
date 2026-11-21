package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "math"
    "testing"
)

func TestGreaterThan_IntegerPassesWhenAboveMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 10)

    if nil != validationError {
        t.Fatalf("expected no error, got: %s", validationError.Error())
    }
}

func TestGreaterThan_IntegerFailsWhenEqualToMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 5)

    if nil == validationError {
        t.Fatalf("expected error when value equals min")
    }

    if ConstraintGreaterThanErrorSmallerThan != validationError.Code() {
        t.Fatalf("expected code %s, got: %s", ConstraintGreaterThanErrorSmallerThan, validationError.Code())
    }
}

func TestGreaterThan_IntegerFailsWhenBelowMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 3)

    if nil == validationError {
        t.Fatalf("expected error when value is below min")
    }
}

func TestGreaterThan_Float64PassesWhenAboveMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 5.5)

    if nil != validationError {
        t.Fatalf("expected no error for float above min, got: %s", validationError.Error())
    }
}

func TestGreaterThan_Float64FailsWhenEqualToMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 5.0)

    if nil == validationError {
        t.Fatalf("expected error when float equals min")
    }
}

func TestGreaterThan_Float64FailsWhenBelowMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 4.9)

    if nil == validationError {
        t.Fatalf("expected error when float is below min")
    }
}

func TestGreaterThan_Float32PassesWhenAboveMin(t *testing.T) {
    constraint := NewGreaterThan(0)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, float32(0.5))

    if nil != validationError {
        t.Fatalf("expected no error for float32 above min, got: %s", validationError.Error())
    }
}

func TestGreaterThan_UintPassesWhenAboveMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(10))

    if nil != validationError {
        t.Fatalf("expected no error for uint above min, got: %s", validationError.Error())
    }
}

func TestGreaterThan_UintFailsWhenEqualToMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(5))

    if nil == validationError {
        t.Fatalf("expected error when uint equals min")
    }
}

func TestGreaterThan_UintAlwaysPassesWhenMinIsNegative(t *testing.T) {
    constraint := NewGreaterThan(-1)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(0))

    if nil != validationError {
        t.Fatalf("expected no error for uint when min is negative, got: %s", validationError.Error())
    }
}

func TestGreaterThan_NilValueReturnsNil(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nil)

    if nil != validationError {
        t.Fatalf("expected nil for nil value")
    }
}

func TestGreaterThan_StringValueReturnsError(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "not a number")

    if nil == validationError {
        t.Fatalf("expected error for non-numeric value")
    }
}

func TestGreaterThan_NilPointerReturnsError(t *testing.T) {
    constraint := NewGreaterThan(5)

    var nilPointer *int

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nilPointer)

    if nil == validationError {
        t.Fatalf("expected error for nil pointer")
    }
}

func TestGreaterThan_PointerToIntPassesWhenAboveMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    value := 10
    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, &value)

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

func TestGreaterThan_RejectsNaN(t *testing.T) {
    constraint := NewGreaterThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, math.NaN())

    if nil == validationError {
        t.Fatalf("expected NaN to be rejected by greaterThan, but it passed validation")
    }
}

func TestGreaterThan_UintFailsWhenBelowMin(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(3))

    if nil == validationError {
        t.Fatalf("expected error when uint is below min")
    }
}

func TestGreaterThan_StringValueNamesTheNumericRequirement(t *testing.T) {
    constraint := NewGreaterThan(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "not a number")

    if nil == validationError || "field: value must be numeric" != validationError.Error() {
        t.Fatalf("expected the numeric requirement to be named, got: %v", validationError)
    }
}

/* a value that is not numeric is refused under its own code, not the bound's. */
func TestGreaterThan_ANonNumericValueIsRefusedUnderItsOwnCode(t *testing.T) {
    validationError := NewGreaterThan(10).Validate(validationcontract.ValidationContext{Field: "field"}, "abc")
    if nil == validationError || ConstraintGreaterThanErrorNotNumeric != validationError.Code() {
        t.Fatalf("expected the not-numeric code, got %v", validationError)
    }
}
