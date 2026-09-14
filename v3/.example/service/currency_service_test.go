package service

import (
    "context"
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

var currencyQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

/* the currency doors under test write through a repository, a cache and a dispatcher. All three are the real
   ones: the storage without a database hands back the in-memory repository the application itself uses when
   it is configured without one, the cache keeps values as they are because these probes are not about
   serialization, and the dispatcher carries a listener so "the event was dispatched" means it arrived. */
func currencyServiceUnderTest(t *testing.T) (*CurrencyService, *recordingDispatcher, melodyruntimecontract.Runtime) {
    t.Helper()

    currencyRepository, repositoryErr := repository.NewCurrencyRepository(persistence.NewCatalogStorage(nil))
    if nil != repositoryErr {
        t.Fatalf("building the repository failed: %v", repositoryErr)
    }

    clockInstance := &frozenClock{instant: currencyQuoteInstant}
    dispatcher := newRecordingDispatcher(clockInstance, event.CurrencyUpdatedEventName)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    /* the framework's dispatcher resolves the logger from the runtime before it runs a listener, so a
       container without one turns every dispatch into a refusal that looks like the door's */
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    return NewCurrencyService(currencyRepository, newTtlRecordingCache(), dispatcher.dispatcher, clockInstance),
        dispatcher,
        melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

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

    /* the listener is what drops the cached list and the cached currency, so a write that skipped the
       dispatch would leave every reader on the old rate */
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

/* the refusal comes before anything is written, which is what the second half asserts: a guard that refused
   after the write would leave the catalogue holding a rate it had just called impossible */
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

/* Create stamps the instant from the injected clock, which is the half a wall-clock read could not be
   asserted on at all */
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
