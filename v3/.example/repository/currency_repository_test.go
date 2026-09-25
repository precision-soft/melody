package repository

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestValidateCurrencyNamesTheFirstFieldItFailsOn(t *testing.T) {
    if validationErr := validateCurrency(nil); nil == validationErr || "currency is required" != validationErr.Error() {
        t.Fatalf("expected an absent currency to be refused, got %v", validationErr)
    }

    if validationErr := validateCurrency(&entity.Currency{Code: " ", Name: " "}); nil == validationErr || "code is required" != validationErr.Error() {
        t.Fatalf("expected a blank code to be refused first, got %v", validationErr)
    }

    if validationErr := validateCurrency(&entity.Currency{Code: "EUR", Name: " "}); nil == validationErr || "name is required" != validationErr.Error() {
        t.Fatalf("expected a blank name to be refused, got %v", validationErr)
    }

    if validationErr := validateCurrency(&entity.Currency{Code: "EUR", Name: "Euro"}); nil != validationErr {
        t.Fatalf("expected a complete currency to pass, got %v", validationErr)
    }
}

func TestNextCurrencyIdContinuesTheSeededNumbering(t *testing.T) {
    if "cur-6" != nextCurrencyId([]string{"cur-1", "cur-5", "cur-2"}) {
        t.Fatalf("expected cur-6, got %q", nextCurrencyId([]string{"cur-1", "cur-5", "cur-2"}))
    }
}
