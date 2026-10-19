package validation

import (
    "testing"
)

func TestCurrencyCode_AdmitsThreeUpperCaseLettersAndLeavesABlankToNotBlank(t *testing.T) {
    constraint := &CurrencyCode{}

    for _, value := range []any{"EUR", "RON", "", nil} {
        if validationErr := constraint.Validate(value, "code"); nil != validationErr {
            t.Fatalf("expected %v admitted, got %v", value, validationErr)
        }
    }
}

func TestCurrencyCode_RefusesAnythingElseWithTheRulesMessage(t *testing.T) {
    constraint := &CurrencyCode{}

    for _, value := range []any{"eur", "EU", "EURO", "E1R", " EUR", 978} {
        validationErr := constraint.Validate(value, "code")
        if nil == validationErr {
            t.Fatalf("expected %v refused", value)
        }

        if "code" != validationErr.Field() || ConstraintCurrencyCodeErrorNotACode != validationErr.Code() {
            t.Fatalf("expected the refusal to name the field and the rule's code, got %q %q", validationErr.Field(), validationErr.Code())
        }
    }
}

func TestNewValidator_CarriesTheCurrencyCodeRule(t *testing.T) {
    type subject struct {
        Code string `validate:"notBlank,currencyCode"`
    }

    if validationErr := NewValidator().Validate(subject{Code: "EUR"}); nil != validationErr {
        t.Fatalf("expected EUR admitted, got %v", validationErr)
    }

    if validationErr := NewValidator().Validate(subject{Code: "eu"}); nil == validationErr {
        t.Fatal("expected eu refused by the registered rule")
    }
}
