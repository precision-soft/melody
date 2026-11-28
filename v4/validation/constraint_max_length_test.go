package validation

import (
    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestMaxLength_PointerToLongStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(4)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, pointerOf("way too long for the limit"))

    if nil == validationError {
        t.Fatalf("fail-open: overlong *string passed maxLength (address length measured instead of value)")
    }
}

func TestMaxLength_NonStringIsRejected(t *testing.T) {
    constraint := NewMaxLength(5)

    validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, 7)

    if nil == validationError {
        t.Fatalf("fail-open: an int passed maxLength through its rendering")
    }
}

func TestMaxLength_WithParametersRefusesNegativeBound(t *testing.T) {
    constraint := NewMaxLength(5)

    configured, withParametersErr := constraint.WithParameters(map[string]string{"value": "-1"})

    if nil == withParametersErr || nil != configured {
        t.Fatalf("expected a negative max length bound to be refused")
    }
}

func TestMaxLength_TheConstructorRefusesANegativeBound(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewMaxLength(-1)
        },
        "max length constraint may not be negative",
    )
}

func TestMaxLength_ZeroIsABoundRatherThanAMistake(t *testing.T) {
    constraint := NewMaxLength(0)

    if nil != constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "") {
        t.Fatalf("expected the empty string to satisfy a zero bound")
    }

    if nil == constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "a") {
        t.Fatalf("expected a single character to exceed a zero bound")
    }
}

func TestMaxLength_MeasuresStringsAndRefusesEverythingElse(t *testing.T) {
    constraint := NewMaxLength(4)

    if 4 != constraint.Max() {
        t.Fatalf("expected the accessor to answer the configured bound, got %d", constraint.Max())
    }

    for _, refusedValue := range []any{42, []byte("ab"), []string{}, map[string]string{}, true, 1.5} {
        validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, refusedValue)
        if nil == validationError {
            t.Fatalf("expected %#v to be refused rather than measured, got a pass", refusedValue)
        }

        if "value must be a string" != validationError.Message() {
            t.Fatalf("expected the type refusal for %#v, got %q", refusedValue, validationError.Message())
        }
    }

    if validationError := constraint.Validate(validationcontract.ValidationContext{Field: "field"}, "abcd"); nil != validationError {
        t.Fatalf("expected a string exactly at the bound to pass, got %v", validationError)
    }
}

func TestMaxLength_AnAbsentValueIsOptionalityRatherThanAZeroLengthString(t *testing.T) {
    if validationError := NewMaxLength(4).Validate(validationcontract.ValidationContext{Field: "field"}, nil); nil != validationError {
        t.Fatalf("expected an absent value to pass the upper bound, got %v", validationError)
    }

    var absentPointer *string
    if validationError := NewMaxLength(4).Validate(validationcontract.ValidationContext{Field: "field"}, absentPointer); nil != validationError {
        t.Fatalf("expected a nil pointer to pass the upper bound, got %v", validationError)
    }

    if validationError := NewMinLength(3).Validate(validationcontract.ValidationContext{Field: "field"}, absentPointer); nil != validationError {
        t.Fatalf("expected a nil pointer to pass the lower bound, got %v", validationError)
    }

    if validationError := NewMinLength(3).Validate(validationcontract.ValidationContext{Field: "field"}, ""); nil == validationError {
        t.Fatalf("expected an empty string to fail the lower bound, which an absent value passes")
    }
}

func TestMaxLength_AMissingValueParameterIsNamedUnderParameters(t *testing.T) {
    _, err := NewMaxLength(1).WithParameters(map[string]string{"other": "2"})

    refusal, isRefusal := err.(*exception.Error)
    if false == isRefusal || nil == refusal.Context()["parameters"] {
        t.Fatalf("expected the refusal to name the parameters under parameters, got %v", err)
    }
}
