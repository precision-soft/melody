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

/* NewMaxLength refuses a negative bound at construction, which is where the tag door beside it already refuses the same typo: a length is never negative, so a negative maximum can only be a mistake, and the constraint it used to build refused every value — the empty string included — with a message naming an impossible limit, which is what the client was handed. The refusal is a panic because a constructor cannot answer an error without changing every caller, and because this is a declaration mistake rather than an input. */
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
        /* a length constraint measures a string, not a Go rendering: fmt-formatting the value measured the digits of a number, the brackets of a slice and the layout of a struct — an empty slice passed max=1 while the payload it stood for did not */
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

    /* a length is never negative, so a negative bound can only be a typo — and it would reject every value with a message naming an impossible limit. The refusal stays here rather than being left to the constructor's panic: a tag is data a request path reads, so its mistakes are answered rather than raised. */
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
