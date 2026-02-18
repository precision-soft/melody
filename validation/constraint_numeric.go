package validation

import (
	"regexp"

	validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
	ConstraintNumeric                = "numeric"
	ConstraintNumericErrorNotNumeric = "notNumeric"
)

var (
	numericRegexInstance = regexp.MustCompile(`^[0-9]+$`)
)

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
		return NewValidationError(field, "this field must contain only numbers", ConstraintNumericErrorNotNumeric, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Numeric)(nil)
