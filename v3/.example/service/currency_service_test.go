package service

import (
    "context"
    "encoding/json"
    "math"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyexception "github.com/precision-soft/melody/v3/exception"
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

    updated, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt)
    if nil != err {
        t.Fatalf("the update failed: %v", err)
    }

    if RateUpdateWritten != outcome {
        t.Fatalf("the door answered outcome %d for a moved quote, wanted RateUpdateWritten", outcome)
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
func TestCurrencyServiceUpdateRate_RefusesAQuoteThatIsNotAUsablePrice(t *testing.T) {
    for _, rate := range []float64{0, -1.0842, math.Inf(1), math.NaN(), math.MaxFloat64, 5e-324, 1e-7, 1e10} {
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

    _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-nope", 1.0842, currencyQuoteInstant)
    if nil != err {
        t.Fatalf("updating a missing currency failed instead of reporting it missing: %v", err)
    }

    if RateUpdateAbsent != outcome {
        t.Errorf("a currency the catalogue does not carry was answered with outcome %d, wanted RateUpdateAbsent", outcome)
    }
}

/* a quote older than the one stored is a replay, and the catalogue keeps its newer reading: nothing is
   written and nothing is dispatched, and the door says which of the two it did */
func TestCurrencyServiceUpdateRate_KeepsTheNewerReadingOverAStaleQuote(t *testing.T) {
    currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

    before, _, _ := currencyService.FindById("cur-usd")
    olderInstant := before.RateAsOf.Add(-time.Hour)

    _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 9.99, olderInstant)
    if nil != err {
        t.Fatalf("a stale quote failed instead of being kept out: %v", err)
    }

    if RateUpdateStale != outcome {
        t.Fatalf("a stale quote was answered with outcome %d, wanted RateUpdateStale", outcome)
    }

    after, _, _ := currencyService.FindById("cur-usd")
    if before.Rate != after.Rate || false == before.RateAsOf.Equal(after.RateAsOf) {
        t.Errorf("a stale quote moved cur-usd from %v@%s to %v@%s", before.Rate, before.RateAsOf, after.Rate, after.RateAsOf)
    }

    if 0 != len(dispatcher.names()) {
        t.Errorf("a stale quote dispatched %v", dispatcher.names())
    }
}

/* the quote the catalogue already holds, at the instant it already holds it, is not written again and
   dispatches nothing — on mysql the full-row UPDATE it used to issue affected zero rows, which the door read
   as the currency having vanished — but a cache that kept the PREVIOUS rate after a failed invalidation has
   its two entries of the currency dropped, so it is healed by the tick that finds nothing to write */
func TestCurrencyServiceUpdateRate_AnswersUnchangedWithoutWritingAndDropsTheCachedCurrency(t *testing.T) {
    currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

    quotedAt := currencyQuoteInstant.Add(time.Hour)
    if _, _, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt); nil != err {
        t.Fatalf("the first update failed: %v", err)
    }

    /* the first write dispatched once and the listener is not wired here, so the invalidation it stands for
       never ran: both entries are planted holding the quote from BEFORE that write, which is the state the
       heal exists for, and must be dropped by the second call, not by an event */
    stale := entity.NewCurrency("cur-usd", "USD", "US Dollar", 1.08, currencyQuoteInstant)
    if setErr := currencyService.cache.Set(CacheKeyCurrencyById("cur-usd"), stale, time.Hour); nil != setErr {
        t.Fatalf("planting the stale by-id entry failed: %v", setErr)
    }
    if setErr := currencyService.cache.Set(CacheKeyCurrencyList, []*entity.Currency{stale}, time.Hour); nil != setErr {
        t.Fatalf("planting the stale list failed: %v", setErr)
    }

    recordingRepository := &updateCountingCurrencyRepository{CurrencyRepository: currencyService.currencyRepository}
    currencyService.currencyRepository = recordingRepository

    cacheInstance := currencyService.cache.(*ttlRecordingCache)
    deletesBefore := cacheInstance.deleteCount()

    _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt)
    if nil != err {
        t.Fatalf("the unchanged update failed: %v", err)
    }

    if RateUpdateUnchanged != outcome {
        t.Fatalf("an unchanged quote was answered with outcome %d, wanted RateUpdateUnchanged", outcome)
    }

    if 0 != recordingRepository.updates.Load() {
        t.Errorf("an unchanged quote issued %d UPDATE statements, wanted none", recordingRepository.updates.Load())
    }

    if 1 != len(dispatcher.names()) {
        t.Errorf("an unchanged quote dispatched %v beyond the first write's one event", dispatcher.names())
    }

    if 2 != cacheInstance.deleteCount()-deletesBefore {
        t.Errorf("an unchanged quote dropped %d cache entries, wanted the currency's two", cacheInstance.deleteCount()-deletesBefore)
    }

    /* the two are the currency's OWN entries — the heal is pinned on which keys, not on how many */
    dropped := cacheInstance.deletedKeyList()
    dropped = dropped[len(dropped)-2:]
    if CacheKeyCurrencyById("cur-usd") != dropped[0] || CacheKeyCurrencyList != dropped[1] {
        t.Errorf("an unchanged quote dropped %v, wanted the by-id entry of cur-usd and the list", dropped)
    }
}

/* a cache that already serves the quote the row holds is left as it is: dropping it on every unchanged tick
   made the server read the currency and the list back from the database and write them into the cache again,
   four reads and four writes per tick for a catalogue that had not moved, twenty-four times a day */
func TestCurrencyServiceUpdateRate_KeepsACacheThatAlreadyServesTheQuote(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    quotedAt := currencyQuoteInstant.Add(time.Hour)
    if _, _, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt); nil != err {
        t.Fatalf("the first update failed: %v", err)
    }

    /* the listener is not wired here, so the reads below plant both entries holding the row as it now is */
    if _, _, err := currencyService.FindById("cur-usd"); nil != err {
        t.Fatalf("reading the currency back failed: %v", err)
    }
    if _, err := currencyService.List(); nil != err {
        t.Fatalf("reading the list back failed: %v", err)
    }

    cacheInstance := currencyService.cache.(*ttlRecordingCache)
    deletesBefore := cacheInstance.deleteCount()

    if _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.0842, quotedAt); nil != err || RateUpdateUnchanged != outcome {
        t.Fatalf("the unchanged update answered %d, %v; wanted RateUpdateUnchanged", outcome, err)
    }

    if dropped := cacheInstance.deleteCount() - deletesBefore; 0 != dropped {
        t.Errorf("an unchanged quote over a cache that serves it dropped %d entries, wanted none: %v", dropped, cacheInstance.deletedKeyList())
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

func TestCurrencyServiceCreate_RefusesAQuoteThatIsNotAUsablePrice(t *testing.T) {
    currencyService, _, runtimeInstance := currencyServiceUnderTest(t)

    for _, rate := range []float64{0, math.Inf(1), 1e308} {
        if _, err := currencyService.Create(runtimeInstance, "cur-gbp", "GBP", "Pound Sterling", rate); nil == err {
            t.Fatalf("a currency quoted at %v was created", rate)
        }
    }

    if _, found, _ := currencyService.FindById("cur-gbp"); true == found {
        t.Error("a refused create left the currency in the catalogue")
    }
}

/* updateCountingCurrencyRepository counts the UPDATE statements a door issues; the value the door answers
   comes from the real repository underneath */
type updateCountingCurrencyRepository struct {
    repository.CurrencyRepository
    updates atomic.Int64
}

func (instance *updateCountingCurrencyRepository) Update(ctx context.Context, currency *entity.Currency) (bool, error) {
    instance.updates.Add(1)

    return instance.CurrencyRepository.Update(ctx, currency)
}

func (instance *updateCountingCurrencyRepository) UpdateQuote(ctx context.Context, id string, rate float64, rateAsOf time.Time) (bool, error) {
    instance.updates.Add(1)

    return instance.CurrencyRepository.UpdateQuote(ctx, id, rate, rateAsOf)
}

/* staleReadCurrencyRepository serves one FindById from a snapshot taken earlier — the row as a concurrent
   process read it before another process wrote a newer quote over it */
type staleReadCurrencyRepository struct {
    repository.CurrencyRepository
    snapshot *entity.Currency
}

func (instance *staleReadCurrencyRepository) FindById(ctx context.Context, id string) (*entity.Currency, bool, error) {
    if nil != instance.snapshot && instance.snapshot.Id == id {
        snapshot := instance.snapshot
        instance.snapshot = nil

        return snapshot, true, nil
    }

    return instance.CurrencyRepository.FindById(ctx, id)
}

/* two processes on one schedule, no lock between them: both read the row, both judge their document newer
   than it, and the one holding the OLDER document writes last. The write is conditional on the row at the
   moment of the write, so the older document is refused and answered as stale, and the row keeps the newer
   quote — where a whole-row write let the older document land */
func TestCurrencyServiceUpdateRate_RefusesAnOlderDocumentThatReadTheRowBeforeANewerOneWroteIt(t *testing.T) {
    currencyService, dispatcher, runtimeInstance := currencyServiceUnderTest(t)

    before, _, _ := currencyService.currencyRepository.FindById(context.Background(), "cur-usd")
    stale := &staleReadCurrencyRepository{CurrencyRepository: currencyService.currencyRepository, snapshot: before}
    currencyService.currencyRepository = stale

    newer := currencyQuoteInstant.Add(2 * time.Hour)
    older := currencyQuoteInstant.Add(time.Hour)

    /* the newer document lands first, through the real read */
    stale.snapshot = nil
    if _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.2, newer); nil != err || RateUpdateWritten != outcome {
        t.Fatalf("the newer document was not written: %d, %v", outcome, err)
    }

    /* the older document judges itself against the row as it read it BEFORE the newer write */
    stale.snapshot = before
    _, outcome, err := currencyService.UpdateRate(runtimeInstance, "cur-usd", 1.1, older)
    if nil != err || RateUpdateStale != outcome {
        t.Fatalf("the older document answered %d, %v; wanted stale", outcome, err)
    }

    stored, _, _ := currencyService.currencyRepository.FindById(context.Background(), "cur-usd")
    if 1.2 != stored.Rate || false == stored.RateAsOf.Equal(newer) {
        t.Fatalf("the older document landed over the newer one: %v at %v", stored.Rate, stored.RateAsOf)
    }

    if 1 != len(dispatcher.names()) {
        t.Fatalf("the refused older document dispatched an event: %v", dispatcher.names())
    }
}

/* the refusal of a rate that is not a finite number carries that rate in its context, and encoding/json
   refuses NaN and both infinities: the json journal then fell back to one text rendering of the WHOLE
   context, so the currency and the bounds beside the rate lost their structure in exactly the record that
   described the refusal. A non-finite rate travels as its text. */
func TestRefuseUnusableRate_CarriesANonFiniteRateAsText(t *testing.T) {
    for rate, spelled := range map[float64]string{math.Inf(1): "+Inf", math.Inf(-1): "-Inf"} {
        assertRefusalContextEncodes(t, refuseUnusableRate("cur-usd", rate), spelled)
    }

    assertRefusalContextEncodes(t, refuseUnusableRate("cur-usd", math.NaN()), "NaN")
}

func assertRefusalContextEncodes(t *testing.T, refusal error, spelled string) {
    t.Helper()

    logContext := melodyexception.LogContext(refusal)
    if _, marshalErr := json.Marshal(logContext); nil != marshalErr {
        t.Fatalf("the refusal's context does not encode: %v", marshalErr)
    }

    if spelled != logContext["rate"] {
        t.Errorf("the refused rate travels as %#v, wanted %q", logContext["rate"], spelled)
    }
}
