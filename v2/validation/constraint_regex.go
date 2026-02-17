package validation

import (
	"regexp"

	validationcontract "github.com/precision-soft/melody/v2/validation/contract"
)

func NewRegex(pattern string) *Regex {
	compiled, err := regexp.Compile(pattern)

	return &Regex{
		pattern:  pattern,
		compiled: compiled,
		err:      err,
	}
}

type Regex struct {
	pattern  string
	compiled *regexp.Regexp
	err      error
}

func (instance *Regex) Pattern() string {
	return instance.pattern
}

func (instance *Regex) Validate(value any, field string) validationcontract.ValidationError {
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

	if nil != instance.err || nil == instance.compiled {
		return NewValidationError(field, "invalid validation pattern", ErrorInvalidPattern, nil)
	}

	if false == instance.compiled.MatchString(stringValue) {
		return NewValidationError(field, "this field does not match the required pattern", ErrorRegexMismatch, nil)
	}

	return nil
}
