package repository

import "testing"

/* The code is checked before the name, so a currency missing both is refused for its code. */
func TestValidateCurrencyNamesTheFieldThatFailed(t *testing.T) {
    for expected, breakIt := range map[string]func(*testing.T) error{
        "code is required": func(t *testing.T) error {
            currency := validCurrency()
            currency.Code = " "

            return validateCurrency(currency)
        },
        "name is required": func(t *testing.T) error {
            currency := validCurrency()
            currency.Name = ""

            return validateCurrency(currency)
        },
        "currency is required": func(t *testing.T) error {
            return validateCurrency(nil)
        },
    } {
        t.Run(expected, func(t *testing.T) {
            err := breakIt(t)

            if nil == err {
                t.Fatalf("the currency was accepted")
            }

            if expected != err.Error() {
                t.Fatalf("got %q, want %q", err.Error(), expected)
            }
        })
    }
}

func TestValidateCurrencyReportsTheCodeBeforeTheName(t *testing.T) {
    currency := validCurrency()
    currency.Code = ""
    currency.Name = ""

    err := validateCurrency(currency)
    if nil == err {
        t.Fatalf("an empty currency was accepted")
    }

    if "code is required" != err.Error() {
        t.Fatalf("got %q, want the code to be reported first", err.Error())
    }
}

func TestNextCurrencyIdContinuesTheSeededNumbering(t *testing.T) {
    if "cur-2" != nextCurrencyId([]string{"cur-1"}) {
        t.Fatalf("unexpected identifier: %q", nextCurrencyId([]string{"cur-1"}))
    }

    if "cur-1" != nextCurrencyId(nil) {
        t.Fatalf("an empty catalogue must start at one, got %q", nextCurrencyId(nil))
    }
}
