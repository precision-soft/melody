package validation

import (
    "errors"
    "regexp"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    validationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

const (
    ConstraintRegex                    = "regex"
    ConstraintRegexErrorMismatch       = "regexMismatch"
    ConstraintRegexErrorInvalidPattern = "invalidPattern"
)

/* the empty pattern compiles to a regular expression that matches every string, so it is refused rather than armed; a pattern meant to match everything says so explicitly */
var errEmptyRegexPattern = errors.New("the empty pattern matches every string; a pattern meant to match everything says so explicitly")

/* NewRegex keeps a pattern that does not compile, and the empty pattern with it, as the constraint's error instead of panicking: Validate then refuses every non-empty value with invalidPattern, and Error answers why, so a rule declared wrong fails closed where it is used rather than validating everything in silence — the empty pattern used to compile and match every string. The tag door (WithParams) refuses the empty pattern before it reaches here. */
func NewRegex(pattern string) *Regex {
    if "" == pattern {
        return &Regex{
            pattern: pattern,
            err:     errEmptyRegexPattern,
        }
    }

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
        return NewValidationError(field, "value must be a string", ConstraintRegexErrorMismatch, nil)
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
    patternString, exists := params["pattern"]
    if false == exists {
        patternString, exists = params["value"]
    }

    if false == exists {
        return nil, exception.NewError(
            "regex constraint requires a pattern or value parameter",
            exceptioncontract.Context{
                "params": params,
            },
            nil,
        )
    }

    /* refused at the tag door with a declaration error, the reason on errEmptyRegexPattern: NewRegex would fail the value closed, but a tag spelled regex= is a mistake to name at parse time */
    if "" == patternString {
        return nil, exception.NewError(
            "regex constraint requires a non-empty pattern",
            exceptioncontract.Context{
                "params": params,
            },
            nil,
        )
    }

    return NewRegex(patternString), nil
}

var _ validationcontract.Constraint = (*Regex)(nil)
var _ validationcontract.ParameterizedConstraint = (*Regex)(nil)
