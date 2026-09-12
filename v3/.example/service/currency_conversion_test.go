package service

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* the three rates the provider quotes, spelled here as the literals they are. The expected conversions below
   are literals too rather than the same arithmetic written twice: derived from these, both sides of every
   assertion would move together and a conversion that multiplied where it should divide would still pass. */
func conversionCurrency(id string, code string, rate float64) *entity.Currency {
    return entity.NewCurrency(id, code, code, rate, time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC))
}

func TestConvertAmount_RestatesAPriceQuotedInTheRateBase(t *testing.T) {
    converted, err := ConvertAmount(
        149.99,
        conversionCurrency("cur-eur", "EUR", 1),
        conversionCurrency("cur-usd", "USD", 1.0842),
    )
    if nil != err {
        t.Fatalf("the conversion failed: %v", err)
    }

    if 162.62 != converted {
        t.Errorf("149.99 EUR came back as %v, wanted 162.62", converted)
    }
}

/* neither currency is the base the rates are quoted against, which is the case that proves the base cancels:
   an implementation that needed to know what the base was could not answer this at all. */
func TestConvertAmount_CancelsTheRateBaseBetweenTwoCurrenciesThatAreNotIt(t *testing.T) {
    converted, err := ConvertAmount(
        100,
        conversionCurrency("cur-usd", "USD", 1.0842),
        conversionCurrency("cur-ron", "RON", 4.9761),
    )
    if nil != err {
        t.Fatalf("the conversion failed: %v", err)
    }

    if 458.97 != converted {
        t.Errorf("100 USD came back as %v, wanted 458.97", converted)
    }
}

func TestConvertAmount_RestatesAPriceIntoTheRateBase(t *testing.T) {
    converted, err := ConvertAmount(
        50,
        conversionCurrency("cur-ron", "RON", 4.9761),
        conversionCurrency("cur-eur", "EUR", 1),
    )
    if nil != err {
        t.Fatalf("the conversion failed: %v", err)
    }

    if 10.05 != converted {
        t.Errorf("50 RON came back as %v, wanted 10.05", converted)
    }
}

/* the source rate is the divisor, so a zero there is the one that would hand a reader an infinity */
func TestConvertAmount_RefusesACurrencyWhoseStoredRateIsNotPositive(t *testing.T) {
    for _, testCase := range []struct {
        name string
        from *entity.Currency
        to   *entity.Currency
    }{
        {
            name: "the source is not quoted",
            from: conversionCurrency("cur-usd", "USD", 0),
            to:   conversionCurrency("cur-eur", "EUR", 1),
        },
        {
            name: "the target is not quoted",
            from: conversionCurrency("cur-eur", "EUR", 1),
            to:   conversionCurrency("cur-usd", "USD", 0),
        },
        {
            name: "the source rate is negative",
            from: conversionCurrency("cur-usd", "USD", -1.0842),
            to:   conversionCurrency("cur-eur", "EUR", 1),
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            converted, err := ConvertAmount(100, testCase.from, testCase.to)
            if nil == err {
                t.Fatalf("the conversion answered %v instead of refusing", converted)
            }
        })
    }
}

func TestConvertAmount_RefusesAConversionMissingACurrency(t *testing.T) {
    if _, err := ConvertAmount(100, nil, conversionCurrency("cur-eur", "EUR", 1)); nil == err {
        t.Error("a conversion with no source currency was answered")
    }

    if _, err := ConvertAmount(100, conversionCurrency("cur-eur", "EUR", 1), nil); nil == err {
        t.Error("a conversion with no target currency was answered")
    }
}

func TestFindCurrencyByCode_AnswersTheCurrencyWhateverCaseTheCallerWrote(t *testing.T) {
    currencies := []*entity.Currency{
        conversionCurrency("cur-eur", "EUR", 1),
        nil,
        conversionCurrency("cur-usd", "USD", 1.0842),
    }

    for _, spelling := range []string{"USD", "usd", "Usd", " usd "} {
        found, exists := FindCurrencyByCode(currencies, spelling)
        if false == exists {
            t.Fatalf("%q matched no currency", spelling)
        }

        if "cur-usd" != found.Id {
            t.Errorf("%q matched %q, wanted cur-usd", spelling, found.Id)
        }
    }
}

func TestFindCurrencyByCode_AnswersNothingForACodeTheCatalogueDoesNotCarry(t *testing.T) {
    currencies := []*entity.Currency{conversionCurrency("cur-eur", "EUR", 1)}

    for _, spelling := range []string{"XXX", "", "   "} {
        if _, exists := FindCurrencyByCode(currencies, spelling); true == exists {
            t.Errorf("%q matched a currency", spelling)
        }
    }
}
