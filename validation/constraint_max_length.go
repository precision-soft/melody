package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    validationcontract "github.com/precision-soft/melody/validation/contract"
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

    stringValue, isString := resolved.(string)
    if false == isString {
        /* @important a length constraint measures a string, not a Go rendering: fmt-formatting the value measured the digits of a number, the brackets of a slice and the layout of a struct — an empty slice passed max=1 while the payload it stood for did not */
        return NewValidationError(field, "value must be a string", ConstraintMaxLengthErrorTooLong, nil)
    }

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

    /* a length is never negative, so a negative bound can only be a typo — and it would reject every value with a message naming an impossible limit */
    if 0 > parsed {
        return nil, exception.NewError(
            "max length parameter must not be negative",
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
