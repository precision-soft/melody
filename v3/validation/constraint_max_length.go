package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

const (
    ConstraintMaxLength             = "max"
    ConstraintMaxLengthErrorTooLong = "tooLong"
)

func NewMaxLength(max int) *MaxLength {
    return &MaxLength{max: max}
}

type MaxLength struct {
    max int
}

func (instance *MaxLength) Validate(value any, field string) validationcontract.ValidationError {
    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue := fmt.Sprintf("%v", resolved)
    length := utf8.RuneCountInString(stringValue)
    if length > instance.max {
        return NewValidationError(
            field,
            fmt.Sprintf("this field must not exceed %d characters", instance.max),
            ConstraintMaxLengthErrorTooLong,
            map[string]any{
                "max":    instance.max,
                "actual": length,
            },
        )
    }

    return nil
}

func (instance *MaxLength) Max() int {
    return instance.max
}

func (instance *MaxLength) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    valueString, exists := params["value"]
    if false == exists {
        return nil, exception.NewError(
            "max length constraint requires a value parameter",
            exceptioncontract.Context{
                "params": params,
            },
            nil,
        )
    }

    parsed, ok := parseIntStrict(valueString)
    if false == ok {
        return nil, exception.NewError(
            "invalid max length parameter",
            exceptioncontract.Context{
                "value": valueString,
            },
            nil,
        )
    }

    return NewMaxLength(parsed), nil
}

var _ validationcontract.Constraint = (*MaxLength)(nil)
var _ validationcontract.ParameterizedConstraint = (*MaxLength)(nil)
