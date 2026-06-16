package validation

import (
    "fmt"

    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
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

var _ validationcontract.Constraint = (*MinLength)(nil)
