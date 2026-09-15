package service

import (
    "math"
    "testing"
    "time"
    "github.com/precision-soft/melody/v3/.example/event"
)

func TestCurrencyServiceUpdateRate_WritesTheQuoteAndTellsTheListeners(t *testing.T) {
    currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

    quotedAt := time.Date(2026, time.September, 7, 9, 30, 0, 0, time.UTC)
    currencyService.clock = &frozenClock{instant: quotedAt}

    updated, found, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt)
    if nil != err {
        t.Fatalf("the update failed: %v", err)
    }

    if false == found {
        t.Fatal("the door reported that cur-usd does not exist")
    }

    if 1.0842 != updated.Rate {
        t.Errorf("cur-usd came back quoted at %v, wanted 1.0842", updated.Rate)
    }

    if quotedAt != updated.RateAsOf.UTC() {
        t.Errorf("cur-usd is stamped %s, wanted the instant the quote was taken at", updated.RateAsOf.UTC())
    }

    dispatched := dispatcher.names()
    if 1 != len(dispatched) || event.CurrencyUpdatedEventName != dispatched[0] {
        t.Errorf("the update dispatched %v, wanted one %s", dispatched, event.CurrencyUpdatedEventName)
    }

    stored, storedFound, storedErr := currencyService.FindById("cur-usd")
    if nil != storedErr || false == storedFound {
        t.Fatalf("reading the currency back failed: %v", storedErr)
    }

    if 1.0842 != stored.Rate {
        t.Errorf("the stored currency is quoted at %v, wanted 1.0842", stored.Rate)
    }
}

func TestCurrencyServiceUpdateRate_RefusesAQuoteThatIsNotPositive(t *testing.T) {
    for _, rate := range []float64{0, -1.0842} {
        currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

        before, _, _ := currencyService.FindById("cur-usd")

        _, _, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", rate, currencyQuoteInstant)
        if nil == err {
            t.Fatalf("a rate of %v was accepted", rate)
        }

        after, _, _ := currencyService.FindById("cur-usd")
        if before.Rate != after.Rate {
            t.Errorf("a refused rate of %v still moved the stored quote from %v to %v", rate, before.Rate, after.Rate)
        }

        if 0 != len(dispatcher.names()) {
            t.Errorf("a refused rate of %v dispatched %v", rate, dispatcher.names())
        }
    }
}

func TestCurrencyServiceUpdateRate_AnswersNotFoundForACurrencyTheCatalogueDoesNotCarry(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    _, found, err := currencyService.UpdateRate(runtimeInstance, "cur-nope", 1.0842, currencyQuoteInstant)
    if nil != err {
        t.Fatalf("updating a missing currency failed instead of reporting it missing: %v", err)
    }

    if true == found {
        t.Error("a currency the catalogue does not carry was reported as updated")
    }
}

func TestCurrencyServiceCreate_StampsTheQuoteWithTheInjectedClock(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    created, err := currencyService.Create(runtimeInstance, "cur-gbp", "GBP", "Pound Sterling", 0.8412)
    if nil != err {
        t.Fatalf("the create failed: %v", err)
    }

    if currencyQuoteInstant != created.RateAsOf.UTC() {
        t.Errorf("the new currency is stamped %s, wanted the clock's instant", created.RateAsOf.UTC())
    }

    if 0.8412 != created.Rate {
        t.Errorf("the new currency is quoted at %v, wanted 0.8412", created.Rate)
    }
}

func TestCurrencyServiceCreate_RefusesAQuoteThatIsNotPositive(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    if _, err := currencyService.Create(runtimeInstance, "cur-gbp", "GBP", "Pound Sterling", 0); nil == err {
        t.Fatal("a currency with no quote was created")
    }

    if _, found, _ := currencyService.FindById("cur-gbp"); true == found {
        t.Error("a refused create left the currency in the catalogue")
    }
}

func TestUpdateRateRejectsInvalidInstantsAndNonFiniteRates(t *testing.T) {
    for _, testCase := range []struct { name string; rate float64; stamp time.Time }{
        {"missing instant", 2, time.Time{}},
        {"future instant", 2, currencyQuoteInstant.Add(time.Second)},
        {"replay", 2, time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)},
        {"infinite rate", math.Inf(1), currencyQuoteInstant},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            serviceInstance, dispatcher, runtimeInstance := currencyServiceUnderTest(t)
            before, _, _ := serviceInstance.currencyRepository.FindById(runtimeInstance.Context(), "cur-usd")
            _, _, err := serviceInstance.UpdateRate(runtimeInstance, "cur-usd", testCase.rate, testCase.stamp)
            if nil == err { t.Fatal("invalid quote accepted") }
            after, _, _ := serviceInstance.currencyRepository.FindById(runtimeInstance.Context(), "cur-usd")
            if before.Rate != after.Rate || false == before.RateAsOf.Equal(after.RateAsOf) || 0 != len(dispatcher.names()) { t.Fatal("invalid quote changed stored data or emitted an event") }
        })
    }
}

func TestUpdateRateStoresUTCAndAllowsIdenticalRetry(t *testing.T) {
    serviceInstance, dispatcher, runtimeInstance := currencyServiceUnderTest(t)
    stamp := currencyQuoteInstant.In(time.FixedZone("provider", 3*60*60))
    for range 2 {
        currency, found, err := serviceInstance.UpdateRate(runtimeInstance, "cur-usd", 2, stamp)
        if nil != err || false == found { t.Fatalf("valid retry failed: found=%v err=%v", found, err) }
        if time.UTC != currency.RateAsOf.Location() || false == stamp.Equal(currency.RateAsOf) { t.Fatalf("stored timestamp is not UTC: %v", currency.RateAsOf) }
    }
    if 2 != len(dispatcher.names()) { t.Fatal("retry must notify invalidation listeners again") }
}
