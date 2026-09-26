package product

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

var conversionQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

func conversionCatalogue() []*entity.Currency {
    return []*entity.Currency{
        entity.NewCurrency("cur-eur", "EUR", "Euro", 1, conversionQuoteInstant),
        entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, conversionQuoteInstant),
        entity.NewCurrency("cur-ron", "RON", "Romanian Leu", 4.9761, conversionQuoteInstant),
    }
}

func conversionProduct() *entity.Product {
    return entity.NewProduct("prod-1", "probe", "probe", "cat-1", 149.99, "cur-eur", 1, conversionQuoteInstant, conversionQuoteInstant)
}

/* the door reads nothing off the runtime, so the request is built without one: what it needs is the query
   bag, and NewRequest fills that from the url at construction. */
func conversionRequest(t *testing.T, target string) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodGet, target, nil),
        map[string]string{"id": "prod-1"},
        nil,
        melodyhttp.NewRequestContext("conversion-door-test", conversionQuoteInstant),
    )
}

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

func TestConvertedPriceFor_StampsTheOlderOfTheTwoQuotes(t *testing.T) {
    olderInstant := conversionQuoteInstant.Add(-3 * time.Hour)
    newerInstant := conversionQuoteInstant.Add(2 * time.Hour)

    for _, testCase := range []struct {
        sourceAsOf time.Time
        targetAsOf time.Time
    }{
        {olderInstant, newerInstant},
        {newerInstant, olderInstant},
    } {
        catalogue := []*entity.Currency{
            entity.NewCurrency("cur-eur", "EUR", "Euro", 1, testCase.sourceAsOf),
            entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, testCase.targetAsOf),
        }

        converted, err := convertedPriceFor(conversionRequest(t, "/products/api/read/prod-1/?currency=USD"), conversionProduct(), catalogue)
        if nil != err || nil == converted {
            t.Fatalf("the door failed: %v", err)
        }

        if olderInstant.Format(time.RFC3339) != converted.RateAsOf {
            t.Errorf("source %s, target %s: the conversion is stamped %q, wanted the older quote %s", testCase.sourceAsOf, testCase.targetAsOf, converted.RateAsOf, olderInstant.Format(time.RFC3339))
        }
    }
}

/* the SHAPE of a query parameter is the client's to choose, so ?currency=USD&currency=RON has to be answered
   rather than refused: the first value wins, as it does at the framework's own Input door. */
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

    /* the caller's mistake, so the door answers it as the caller's 400 */
    var serverRefusal *conversionRefusal
    if true == errors.As(err, &serverRefusal) {
        t.Errorf("an unknown code was marked as the server's refusal")
    }
}

/* the product's own currency is looked up in the same list, so a catalogue that lost it refuses rather than
   converting from a zero-valued currency it invented */
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

    /* the catalogue's fault, not the caller's — the caller sent a code the catalogue carries — so the door
       answers it as a 500 with the cause journaled, not as a 400 the caller cannot act on */
    var serverRefusal *conversionRefusal
    if false == errors.As(err, &serverRefusal) {
        t.Errorf("a product quoted in a missing currency was refused as the caller's mistake: %v", err)
    }
}

/* the same distinction for a stored rate that is not a usable price: the caller cannot fix the row */
func TestConvertedPriceFor_MarksAnUnusableStoredRateAsTheServersRefusal(t *testing.T) {
    catalogue := []*entity.Currency{
        entity.NewCurrency("cur-eur", "EUR", "Euro", 0, conversionQuoteInstant),
        entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.0842, conversionQuoteInstant),
    }

    _, err := convertedPriceFor(
        conversionRequest(t, "/products/api/read/prod-1/?currency=USD"),
        conversionProduct(),
        catalogue,
    )
    if nil == err {
        t.Fatal("a product priced against a zero rate was converted")
    }

    var serverRefusal *conversionRefusal
    if false == errors.As(err, &serverRefusal) {
        t.Errorf("a zero stored rate was refused as the caller's mistake: %v", err)
    }
}
