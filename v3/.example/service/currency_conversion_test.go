package service

import (
    "encoding/json"
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyexception "github.com/precision-soft/melody/v3/exception"
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

/* the write doors bound the rates they admit, but a row this application did not write can hold a rate that
   multiplies into an infinity; the read door refuses it instead of handing the serializer a value it
   cannot render */
func TestConvertAmount_RefusesAConvertedAmountThatIsNotFinite(t *testing.T) {
    from := entity.NewCurrency("cur-tiny", "TNY", "Tiny", 5e-324, time.Time{})
    to := entity.NewCurrency("cur-huge", "HGE", "Huge", 1e308, time.Time{})

    if converted, err := ConvertAmount(100, from, to); nil == err {
        t.Fatalf("a conversion that overflows answered %v with no error", converted)
    }

    if converted, err := ConvertAmount(100, to, to); nil != err || 100 != converted {
        t.Errorf("an identity conversion over a huge rate answered %v, %v; wanted 100", converted, err)
    }
}

/* the guard judges the rounded value: the rounding multiplies by a hundred first, so a finite product within a hundredth of the float ceiling becomes an infinity after a guard on the product would let it through */
func TestConvertAmount_RefusesAConvertedAmountThatOnlyTheRoundingOverflows(t *testing.T) {
    from := entity.NewCurrency("cur-one", "ONE", "One", 1, time.Time{})
    to := entity.NewCurrency("cur-huge", "HGE", "Huge", 1e9, time.Time{})

    /* 1e300 × 1e9 = 1e309 overflows outright; math.MaxFloat64/1e9/50 × 1e9 is finite and its hundredfold is not */
    amount := math.MaxFloat64 / 1e9 / 50

    if converted, err := ConvertAmount(amount, from, to); nil == err {
        t.Fatalf("a conversion whose rounding overflows answered %v with no error", converted)
    }
}

/* the refusal of a converted amount that is not finite can carry an amount that is not finite either, and its
   context still encodes: the json journal would otherwise render the whole context as one text */
func TestConvertAmount_ARefusalOfANonFiniteAmountEncodes(t *testing.T) {
    one := conversionCurrency("cur-one", "ONE", 1)

    _, err := ConvertAmount(math.Inf(1), one, one)
    if nil == err {
        t.Fatal("an infinite amount was converted")
    }

    logContext := melodyexception.LogContext(err)
    if _, marshalErr := json.Marshal(logContext); nil != marshalErr {
        t.Fatalf("the refusal's context does not encode: %v", marshalErr)
    }

    if "+Inf" != logContext["amount"] {
        t.Errorf("the amount travels as %#v, wanted \"+Inf\"", logContext["amount"])
    }
}

/* a code is three ASCII letters, so the fold upper-cases ASCII alone: the standard library's ToUpper maps U+017F LATIN SMALL LETTER LONG S onto S, which would make a provider's "uſd" name the catalogue's USD. A code carrying a control byte keeps it and so matches nothing the catalogue holds. */
func TestFoldCurrencyCode_FoldsAsciiAlone(t *testing.T) {
    for code, expected := range map[string]string{
        " usd ":   "USD",
        "Usd":     "USD",
        "u\u017fd": "U\u017fD",
        "u\x00sd": "U\x00SD",
    } {
        if folded := foldCurrencyCode(code); expected != folded {
            t.Errorf("foldCurrencyCode(%q) = %q, wanted %q", code, folded, expected)
        }
    }

    currencies := []*entity.Currency{conversionCurrency("cur-usd", "USD", 1.1)}
    if found, matched := FindCurrencyByCode(currencies, "u\u017fd"); true == matched {
        t.Errorf("a code spelled with a long s matched %s", found.Code)
    }
}
