package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintMinLength                        = "min"
    ConstraintMinLengthErrorInsufficientLength = "insufficientLength"
)

func NewMinLength(min int) *MinLength {
    return &MinLength{min: min}
}

type MinLength struct {
    min int
}

func (instance *MinLength) Validate(value any, field string) validationcontract.ValidationError {
    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        /* @important a length constraint measures a string, not a Go rendering: fmt-formatting the value measured the digits of a number, the brackets of a slice and the layout of a struct — an empty slice passed min=1 because its rendering [] is two runes long */
        return NewValidationError(field, "value must be a string", ConstraintMinLengthErrorInsufficientLength, nil)
    }

    length := utf8.RuneCountInString(stringValue)
    if length < instance.min {
        return NewValidationError(
            field,
            fmt.Sprintf("this field must be at least %d characters long", instance.min),
            ConstraintMinLengthErrorInsufficientLength,
            map[string]any{
                "min":    instance.min,
                "actual": length,
            },
        )
    }

    return nil
}

func (instance *MinLength) Min() int {
    return instance.min
}

func (instance *MinLength) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    valueString, exists := params["value"]
    if false == exists {
        return nil, exception.NewError(
            "min length constraint requires a value parameter",
            exceptioncontract.Context{
                "params": params,
            },
            nil,
        )
    }

    parsed, ok := parseIntStrict(valueString)
    if false == ok {
        return nil, exception.NewError(
            "invalid min length parameter",
            exceptioncontract.Context{
                "value": valueString,
            },
            nil,
        )
    }

    /* a length is never negative, so a negative bound can only be a typo — and it would make the rule a silent no-op that still looks enforced in the tag */
    if 0 > parsed {
        return nil, exception.NewError(
            "min length parameter must not be negative",
            exceptioncontract.Context{
                "value": valueString,
            },
            nil,
        )
    }

    return NewMinLength(parsed), nil
}

var _ validationcontract.Constraint = (*MinLength)(nil)
var _ validationcontract.ParameterizedConstraint = (*MinLength)(nil)
