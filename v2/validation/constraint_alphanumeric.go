package validation

import (
	"regexp"

	validationcontract "github.com/precision-soft/melody/v2/validation/contract"
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

	stringValue, ok := value.(string)
	if false == ok {
		return nil
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
