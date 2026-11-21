package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "math"
    "testing"
)

func TestLessThan_IntegerPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 5)

    if nil != validationError {
        t.Fatalf("expected no error, got: %s", validationError.Error())
    }
}

func TestLessThan_IntegerFailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 10)

    if nil == validationError {
        t.Fatalf("expected error when value equals max")
    }

    if ConstraintLessThanErrorGreaterThan != validationError.Code() {
        t.Fatalf("expected code %s, got: %s", ConstraintLessThanErrorGreaterThan, validationError.Code())
    }
}

func TestLessThan_IntegerFailsWhenAboveMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 15)

    if nil == validationError {
        t.Fatalf("expected error when value is above max")
    }
}

func TestLessThan_Float64PassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 9.5)

    if nil != validationError {
        t.Fatalf("expected no error for float below max, got: %s", validationError.Error())
    }
}

func TestLessThan_Float64FailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 10.0)

    if nil == validationError {
        t.Fatalf("expected error when float equals max")
    }
}

func TestLessThan_UintPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(5))

    if nil != validationError {
        t.Fatalf("expected no error for uint below max, got: %s", validationError.Error())
    }
}

func TestLessThan_UintFailsWhenEqualToMax(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(10))

    if nil == validationError {
        t.Fatalf("expected error when uint equals max")
    }
}

func TestLessThan_UintAlwaysFailsWhenMaxIsNegative(t *testing.T) {
    constraint := NewLessThan(-1)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, uint(0))

    if nil == validationError {
        t.Fatalf("expected error for uint when max is negative")
    }
}

func TestLessThan_NilValueReturnsNil(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nil)

    if nil != validationError {
        t.Fatalf("expected nil for nil value")
    }
}

func TestLessThan_StringValueReturnsError(t *testing.T) {
    constraint := NewLessThan(10)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "not a number")

    if nil == validationError {
        t.Fatalf("expected error for non-numeric value")
    }
}

func TestLessThan_NilPointerReturnsError(t *testing.T) {
    constraint := NewLessThan(10)

    var nilPointer *int

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, nilPointer)

    if nil == validationError {
        t.Fatalf("expected error for nil pointer")
    }
}

func TestLessThan_PointerToIntPassesWhenBelowMax(t *testing.T) {
    constraint := NewLessThan(10)

    value := 5
    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, &value)

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

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, math.NaN())

    if nil == validationError {
        t.Fatalf("expected NaN to be rejected by lessThan, but it passed validation")
    }
}

func TestLessThan_RefusesAParameterThatIsNotAnIntegerInItsEntirety(t *testing.T) {
    constraint := NewLessThan(0)

    for _, refusedValue := range []string{"10a", "1e3", "-0.5", "", " ", "0x10", "1 2"} {
        built, buildErr := constraint.WithParameters(map[string]string{"value": refusedValue})
        if nil == buildErr {
            t.Fatalf("expected %q to be refused as a bound, got %#v", refusedValue, built)
        }

        if "invalid less than parameter" != buildErr.Error() {
            t.Fatalf("unexpected refusal for %q: %v", refusedValue, buildErr)
        }

        if nil != built {
            t.Fatalf("expected no constraint to be built from %q, got %#v", refusedValue, built)
        }
    }

    built, buildErr := constraint.WithParameters(map[string]string{"value": "-5"})
    if nil != buildErr {
        t.Fatalf("expected a negative integer bound to be accepted, got %v", buildErr)
    }

    if -5 != built.(*LessThan).Max() {
        t.Fatalf("expected the parsed bound to be carried, got %d", built.(*LessThan).Max())
    }
}

/* a value that is not numeric is refused under its own code, not the bound's: a consumer switching on the code does not read a string as a number above the maximum. */
func TestLessThan_ANonNumericValueIsRefusedUnderItsOwnCode(t *testing.T) {
    validationError := NewLessThan(10).Validate(validationcontract.ValidationContext{Field: "field"}, "abc")
    if nil == validationError || ConstraintLessThanErrorNotNumeric != validationError.Code() {
        t.Fatalf("expected the not-numeric code, got %v", validationError)
    }
}
