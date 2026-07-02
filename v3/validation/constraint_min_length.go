package validation

import (
    "fmt"

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
    if len(stringValue) < instance.min {
        return NewValidationError(
            field,
            fmt.Sprintf("this field must be at least %d characters long", instance.min),
            ConstraintMinLengthErrorInsufficientLength,
            map[string]any{
                "min":    instance.min,
                "actual": len(stringValue),
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
        return NewMinLength(instance.min), nil
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
