package validation

import (
    "regexp"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    validationcontract "github.com/precision-soft/melody/validation/contract"
)

const (
    ConstraintRegex                    = "regex"
    ConstraintRegexErrorMismatch       = "regexMismatch"
    ConstraintRegexErrorInvalidPattern = "invalidPattern"
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

func (instance *Regex) Validate(value any, field string) validationcontract.ValidationError {
    if nil == value {
        return nil
    }

    resolved, ok := dereferenceValue(value)
    if false == ok {
        return nil
    }

    stringValue, isString := resolved.(string)
    if false == isString {
        return nil
    }

    if "" == stringValue {
        return nil
    }

    if nil != instance.err || nil == instance.compiled {
        return NewValidationError(field, "invalid validation pattern", ConstraintRegexErrorInvalidPattern, nil)
    }

    if false == instance.compiled.MatchString(stringValue) {
        return NewValidationError(field, "this field does not match the required pattern", ConstraintRegexErrorMismatch, nil)
    }

    return nil
}

func (instance *Regex) Pattern() string {
    return instance.pattern
}

func (instance *Regex) Compiled() *regexp.Regexp {
    return instance.compiled
}

func (instance *Regex) Error() error {
    return instance.err
}

func (instance *Regex) WithParams(params map[string]string) (validationcontract.Constraint, error) {
    if patternString, exists := params["pattern"]; true == exists {
        return NewRegex(patternString), nil
    }

    if patternString, exists := params["value"]; true == exists {
        return NewRegex(patternString), nil
    }

    return nil, exception.NewError(
        "regex constraint requires a pattern or value parameter",
        exceptioncontract.Context{
            "params": params,
        },
        nil,
    )
}

var _ validationcontract.Constraint = (*Regex)(nil)
var _ validationcontract.ParameterizedConstraint = (*Regex)(nil)
