package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
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

    stringValue := fmt.Sprintf("%v", resolved)
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

    return NewMinLength(parsed), nil
}

var _ validationcontract.Constraint = (*MinLength)(nil)
var _ validationcontract.ParameterizedConstraint = (*MinLength)(nil)
