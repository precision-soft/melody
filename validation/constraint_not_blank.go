package validation

import (
    "fmt"
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

    stringValue := fmt.Sprintf("%v", resolved)
    if "" == strings.TrimSpace(stringValue) {
        return NewValidationError(field, "this field is required", ConstraintNotBlankErrorIsBlank, nil)
    }

    return nil
}

var _ validationcontract.Constraint = (*NotBlank)(nil)
