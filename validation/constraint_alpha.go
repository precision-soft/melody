package validation

import (
	"regexp"

	validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
	ConstraintAlpha              = "alpha"
	ConstraintAlphaErrorNotAlpha = "notAlpha"
)

var (
	alphaRegexInstance = regexp.MustCompile(`^[a-zA-Z]+$`)
)

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
		return NewValidationError(field, "this field must contain only letters", ConstraintAlphaErrorNotAlpha, nil)
	}

	return nil
}

var _ validationcontract.Constraint = (*Alpha)(nil)
