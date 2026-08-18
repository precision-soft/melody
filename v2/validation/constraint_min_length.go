package validation

import (
    "fmt"
    "unicode/utf8"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

const (
    ConstraintMinLength                        = "min"
    ConstraintMinLengthErrorInsufficientLength = "insufficientLength"
)

/* NewMinLength refuses a negative bound at construction, which is where the tag door beside it already refuses the same typo: a length is never negative, so a negative minimum can only be a mistake, and the constraint it used to build accepted every value in silence — a validation rule that reads as enforced and validates nothing, with no record anywhere. The refusal is a panic because a constructor cannot answer an error without changing every caller, and because this is a declaration mistake rather than an input: it is the shape the cron runner already uses for the same class, and the deliberation a warning presumes is missing when the rule is written wrong. */
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
        /* a length constraint measures a string, not a Go rendering: fmt-formatting the value measured the digits of a number, the brackets of a slice and the layout of a struct — an empty slice passed min=1 because its rendering [] is two runes long */
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

    /* a length is never negative, so a negative bound can only be a typo — and it would make the rule a silent no-op that still looks enforced in the tag. The refusal stays here rather than being left to the constructor's panic: a tag is data a request path reads, so its mistakes are answered rather than raised. */
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
