package validation

import (
    "regexp"

    melodyvalidation "github.com/precision-soft/melody/v3/validation"
    melodyvalidationcontract "github.com/precision-soft/melody/v3/validation/contract"
)

const (
    ConstraintCurrencyCode              = "currencyCode"
    ConstraintCurrencyCodeErrorNotACode = "notCurrencyCode"
)

var currencyCodeRegexInstance = regexp.MustCompile(`^[A-Z]{3}$`)

/* CurrencyCode is the rule of a currency's code: three upper-case letters, the shape of an ISO 4217 alphabetic code. A blank value is left to notBlank, so the two rules report one refusal each. */
type CurrencyCode struct{}

func (instance *CurrencyCode) Validate(value any, field string) melodyvalidationcontract.ValidationError {
    if nil == value {
        return nil
    }

    stringValue, isString := value.(string)
    if false == isString {
        return melodyvalidation.NewValidationError(field, "value must be a string", ConstraintCurrencyCodeErrorNotACode, nil)
    }

    if "" == stringValue {
        return nil
    }

    if false == currencyCodeRegexInstance.MatchString(stringValue) {
        return melodyvalidation.NewValidationError(field, "must be a three-letter upper-case ISO 4217 code", ConstraintCurrencyCodeErrorNotACode, nil)
    }

    return nil
}

var _ melodyvalidationcontract.Constraint = (*CurrencyCode)(nil)

/* NewValidator is the framework's validator carrying this application's own rules, the one the container serves and the tests bind with */
func NewValidator() *melodyvalidation.Validator {
    validator := melodyvalidation.NewValidator()
    validator.RegisterConstraint(ConstraintCurrencyCode, &CurrencyCode{})

    return validator
}
