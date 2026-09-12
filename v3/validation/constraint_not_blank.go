package validation

import (
    "strings"

    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

const (
    ConstraintNotBlank             = "notBlank"
    ConstraintNotBlankErrorIsBlank = "isBlank"
)

/* NotBlank judges a string. A value of any other type is refused rather than skipped; notEmpty is the constraint that understands collections. */
type NotBlank struct{}

func (instance *NotBlank) Validate(value any, field string) validationcontract.ValidationError {
    resolved, ok := dereferenceValue(value)
    if false == ok {
        return NewValidationError(field, "this field is required", ConstraintNotBlankErrorIsBlank, nil)
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        return NewValidationError(field, "value must be a string", ConstraintNotBlankErrorIsBlank, nil)
    }

    if "" == strings.TrimSpace(stringValue) {
        return NewValidationError(field, "this field is required", ConstraintNotBlankErrorIsBlank, nil)
    }

    return nil
}

var _ validationcontract.Constraint = (*NotBlank)(nil)
