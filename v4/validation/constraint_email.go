package validation

import (
    "regexp"

    validationcontract "github.com/precision-soft/melody/v4/validation/contract"
)

const (
    ConstraintEmail                  = "email"
    ConstraintEmailErrorInvalidEmail = "invalidEmail"
)

var (
    emailRegexInstance = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

type Email struct{}

func (instance *Email) Validate(validationContext validationcontract.ValidationContext, value any) validationcontract.ValidationError {
    if nil == value {
        return nil
    }

    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        return NewValidationError(validationContext.Field, "value must be a string", ConstraintEmailErrorInvalidEmail, nil)
    }

    if "" == stringValue {
        return nil
    }

    if false == emailRegexInstance.MatchString(stringValue) {
        return NewValidationError(validationContext.Field, "invalid email format", ConstraintEmailErrorInvalidEmail, nil)
    }

    return nil
}

var _ validationcontract.Constraint = (*Email)(nil)
