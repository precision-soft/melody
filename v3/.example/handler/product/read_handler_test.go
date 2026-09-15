package product

import (
    "strings"
    "testing"
    "time"
    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestConvertedPriceFor_AnswersNothingWhenTheCallerAskedForNoConversion(t *testing.T) {
    for _, target := range []string{
        "/products/api/read/prod-1/",
        "/products/api/read/prod-1/?currency=",
        "/products/api/read/prod-1/?currency=%20%20",
        "/products/api/read/prod-1/?other=USD",
    } {
        converted, err := convertedPriceFor(conversionRequest(t, target), conversionProduct(), conversionCatalogue())
        if nil != err {
            t.Fatalf("%s: the door failed: %v", target, err)
        }

        if nil != converted {
            t.Errorf("%s: the door converted into %+v without being asked", target, converted)
        }
    }
}

func TestConvertedPriceFor_RestatesThePriceInTheCurrencyTheCallerNamed(t *testing.T) {
    converted, err := convertedPriceFor(
        conversionRequest(t, "/products/api/read/prod-1/?currency=USD"),
        conversionProduct(),
        conversionCatalogue(),
    )
    if nil != err {
        t.Fatalf("the door failed: %v", err)
    }

    if nil == converted {
        t.Fatal("the door answered no conversion")
    }

    if 162.62 != converted.Price {
        t.Errorf("149.99 EUR came back as %v, wanted 162.62", converted.Price)
    }

    if "cur-usd" != converted.CurrencyId || "USD" != converted.Code {
        t.Errorf("the conversion names %q/%q, wanted cur-usd/USD", converted.CurrencyId, converted.Code)
    }

    if "2026-09-07T09:00:00Z" != converted.RateAsOf {
        t.Errorf("the conversion is stamped %q, wanted the instant of the quote it used", converted.RateAsOf)
    }
}

func TestConvertedPriceFor_TakesTheFirstValueOfARepeatedParameter(t *testing.T) {
    converted, err := convertedPriceFor(
        conversionRequest(t, "/products/api/read/prod-1/?currency=USD&currency=RON"),
        conversionProduct(),
        conversionCatalogue(),
    )
    if nil != err {
        t.Fatalf("a repeated parameter failed the door: %v", err)
    }

    if nil == converted {
        t.Fatal("a repeated parameter produced no conversion")
    }

    if "USD" != converted.Code {
        t.Errorf("the door converted into %q, wanted the first value, USD", converted.Code)
    }
}

func TestConvertedPriceFor_RefusesACurrencyTheCatalogueDoesNotCarry(t *testing.T) {
    converted, err := convertedPriceFor(
        conversionRequest(t, "/products/api/read/prod-1/?currency=XXX"),
        conversionProduct(),
        conversionCatalogue(),
    )
    if nil == err {
        t.Fatalf("an unknown code was converted into %+v", converted)
    }

    if false == strings.Contains(err.Error(), "XXX") {
        t.Errorf("the refusal reads %q and does not name the code that was refused", err.Error())
    }
}

func TestConvertedPriceFor_RefusesWhenTheProductsOwnCurrencyIsMissing(t *testing.T) {
    catalogue := []*entity.Currency{
        entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, conversionQuoteInstant),
    }

    converted, err := convertedPriceFor(
        conversionRequest(t, "/products/api/read/prod-1/?currency=USD"),
        conversionProduct(),
        catalogue,
    )
    if nil == err {
        t.Fatalf("a product quoted in a missing currency was converted into %+v", converted)
    }
}

func TestConversionRefusalDistinguishesClientAndCatalogueFailures(t *testing.T) {
    for _, testCase := range []struct { name, code string; removeSource bool; expected int }{
        {"unknown client code", "ZZZ", false, 400},
        {"missing source in catalogue", "USD", true, 500},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            catalogue := conversionCatalogue()
            if testCase.removeSource { catalogue = catalogue[1:] }
            request := conversionRequest(t, "/products/api/read/prod-1/?currency="+testCase.code)
            _, err := convertedPriceFor(request, conversionProduct(), catalogue)
            if nil == err { t.Fatal("expected conversion refusal") }
            response := conversionErrorResponse(nil, request, err)
            if testCase.expected != response.StatusCode() { t.Fatalf("status=%d want=%d", response.StatusCode(), testCase.expected) }
        })
    }
}

func TestConvertedPriceForReportsTheOlderInputQuote(t *testing.T) {
    for _, olderIndex := range []int{0, 1} {
        currencies := conversionCatalogue()
        older := conversionQuoteInstant.Add(-time.Hour)
        currencies[olderIndex].RateAsOf = older
        converted, convertErr := convertedPriceFor(conversionRequest(t, "/products/api/read/prod-1/?currency=USD"), conversionProduct(), currencies)
        if nil != convertErr || nil == converted {
            t.Fatalf("conversion failed: %v", convertErr)
        }
        if older.Format(time.RFC3339) != converted.RateAsOf {
            t.Fatalf("older input %d ignored: got %s", olderIndex, converted.RateAsOf)
        }
    }
}
