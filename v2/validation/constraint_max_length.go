package validation

import (
	"fmt"

	validationcontract "github.com/precision-soft/melody/v2/validation/contract"
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
	if nil == value {
		return nil
	}

	stringValue := fmt.Sprintf("%v", value)
	if len(stringValue) > instance.max {
		return NewValidationError(
			field,
			fmt.Sprintf("this field must not exceed %d characters", instance.max),
			ConstraintMaxLengthErrorTooLong,
			map[string]any{
				"max":    instance.max,
				"actual": len(stringValue),
			},
		)
	}

	return nil
}

func (instance *MaxLength) Max() int {
	return instance.max
}

var _ validationcontract.Constraint = (*MaxLength)(nil)
