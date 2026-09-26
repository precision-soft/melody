package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

const (
    ConstraintMaxLength             = "max"
    ConstraintMaxLengthErrorTooLong = "tooLong"
)

/* NewMaxLength panics on a negative bound: a length is never negative, so it is a declaration mistake, and the constraint would refuse every value under an impossible limit. */
func NewMaxLength(max int) *MaxLength {
    if 0 > max {
        exception.Panic(
            exception.NewError(
                "max length constraint may not be negative",
                exceptioncontract.Context{
                    "max": max,
                },
                nil,
            ),
        )
    }

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
        /* a length constraint measures a string, never a Go rendering of another value */
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

    /* a negative bound is a typo that would reject every value; a tag is data the request path reads, so it is refused as an error here rather than by the constructor's panic */
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
