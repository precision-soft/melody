package validation

import (
	"fmt"
	"regexp"
	"strings"

	validationcontract "github.com/precision-soft/melody/validation/contract"
)

var (
	emailRegexInstance        = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	numericRegexInstance      = regexp.MustCompile(`^[0-9]+$`)
	alphaRegexInstance        = regexp.MustCompile(`^[a-zA-Z]+$`)
	alphanumericRegexInstance = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
)

type NotBlank struct{}

func (instance *NotBlank) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return NewValidationError(field, "this field is required", ErrorNotBlank, nil)
	}

	stringValue := fmt.Sprintf("%v", value)
	if "" == strings.TrimSpace(stringValue) {
		return NewValidationError(field, "this field is required", ErrorNotBlank, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*NotBlank)(nil)

type Email struct{}

func (instance *Email) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue, ok := value.(string)
	if false == ok {
		return nil
	}

	if "" == stringValue {
		return nil
	}

	if false == emailRegexInstance.MatchString(stringValue) {
		return NewValidationError(field, "invalid email format", ErrorInvalidEmail, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Email)(nil)

func NewMinLength(min int) *MinLength {
	return &MinLength{min: min}
}

type MinLength struct {
	min int
}

func (instance *MinLength) Min() int {
	return instance.min
}

func (instance *MinLength) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue := fmt.Sprintf("%v", value)
	min := instance.Min()
	if len(stringValue) < min {
		return NewValidationError(
			field,
			fmt.Sprintf("this field must be at least %d characters long", min),
			ErrorMinLength,
			map[string]any{
				"min":    min,
				"actual": len(stringValue),
			},
		)
	}

	return nil
}

var _ validationcontract.Constraint = (*MinLength)(nil)

func NewMaxLength(max int) *MaxLength {
	return &MaxLength{max: max}
}

type MaxLength struct {
	max int
}

func (instance *MaxLength) Max() int {
	return instance.max
}

func (instance *MaxLength) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue := fmt.Sprintf("%v", value)
	max := instance.Max()
	if len(stringValue) > max {
		return NewValidationError(
			field,
			fmt.Sprintf("this field must not exceed %d characters", max),
			ErrorMaxLength,
			map[string]any{
				"max":    max,
				"actual": len(stringValue),
			},
		)
	}

	return nil
}

var _ validationcontract.Constraint = (*MaxLength)(nil)

type Numeric struct{}

func (instance *Numeric) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue, ok := value.(string)
	if false == ok {
		return nil
	}

	if "" == stringValue {
		return nil
	}

	if false == numericRegexInstance.MatchString(stringValue) {
		return NewValidationError(field, "this field must contain only numbers", ErrorNotNumeric, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Numeric)(nil)

type Alpha struct{}

func (instance *Alpha) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue, ok := value.(string)
	if false == ok {
		return nil
	}

	if "" == stringValue {
		return nil
	}

	if false == alphaRegexInstance.MatchString(stringValue) {
		return NewValidationError(field, "this field must contain only letters", ErrorNotAlpha, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Alpha)(nil)

type Alphanumeric struct{}

func (instance *Alphanumeric) Validate(value any, field string) validationcontract.ValidationError {
	if nil == value {
		return nil
	}

	stringValue, ok := value.(string)
	if false == ok {
		return nil
	}

	if "" == stringValue {
		return nil
	}

	if false == alphanumericRegexInstance.MatchString(stringValue) {
		return NewValidationError(field, "this field must contain only letters and numbers", ErrorNotAlphanumeric, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Alphanumeric)(nil)
