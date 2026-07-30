package validation

import (
    "strings"

    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintNotBlank             = "notBlank"
    ConstraintNotBlankErrorIsBlank = "isBlank"
)

type NotBlank struct{}

func (instance *NotBlank) Validate(value any, field string) validationcontract.ValidationError {
    resolved, ok := dereferenceValue(value)
    if false == ok {
        return NewValidationError(field, "this field is required", ConstraintNotBlankErrorIsBlank, nil)
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        /* @important blankness is a property of a string: fmt-formatting anything else produced text that is never blank, so notBlank on a bool, a number or a collection accepted every value including false, 0 and the empty slice — notEmpty is the constraint that understands collections */
        return NewValidationError(field, "value must be a string", ConstraintNotBlankErrorIsBlank, nil)
    }

    if "" == strings.TrimSpace(stringValue) {
        return NewValidationError(field, "this field is required", ConstraintNotBlankErrorIsBlank, nil)
    }

    return nil
}

var _ validationcontract.Constraint = (*NotBlank)(nil)
