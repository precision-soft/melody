package validation

import (
    "regexp"

    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintAlphanumeric                     = "alphanumeric"
    ConstraintAlphanumericErrorNotAlphanumeric = "notAlphanumeric"
)

var (
    alphanumericRegexInstance = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
)

type Alphanumeric struct{}

func (instance *Alphanumeric) Validate(value any, field string) validationcontract.ValidationError {
    if nil == value {
        return nil
    }

    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        /* @important fail closed on a type the character rule can never hold: alphanumeric on a non-string field silently enforced nothing, while the numeric constraints refuse the types they cannot interpret */
        return NewValidationError(field, "value must be a string", ConstraintAlphanumericErrorNotAlphanumeric, nil)
    }

    if "" == stringValue {
        return nil
    }

    if false == alphanumericRegexInstance.MatchString(stringValue) {
        return NewValidationError(field, "this field must contain only letters and numbers", ConstraintAlphanumericErrorNotAlphanumeric, nil)
    }

    return nil
}

var _ validationcontract.Constraint = (*Alphanumeric)(nil)
