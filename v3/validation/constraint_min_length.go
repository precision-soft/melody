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

/* NewMinLength panics on a negative bound: a length is never negative, so it is a declaration mistake, and the constraint would accept every value while reading as enforced. */
func NewMinLength(min int) *MinLength {
    if 0 > min {
        exception.Panic(
            exception.NewError(
                "min length constraint may not be negative",
                exceptioncontract.Context{
                    "min": min,
                },
                nil,
            ),
        )
    }

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
        /* a length constraint measures a string, never a Go rendering of another value */
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

    /* a negative bound is a typo that would make the rule a silent no-op; a tag is data the request path reads, so it is refused as an error here rather than by the constructor's panic */
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
