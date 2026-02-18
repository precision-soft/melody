package validation

import (
	"regexp"

	validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
	ConstraintEmail                  = "email"
	ConstraintEmailErrorInvalidEmail = "invalidEmail"
)

var (
	emailRegexInstance = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

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
		return NewValidationError(field, "invalid email format", ConstraintEmailErrorInvalidEmail, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Email)(nil)
