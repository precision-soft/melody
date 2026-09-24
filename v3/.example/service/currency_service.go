package service

import (
    "context"
    "errors"
    "fmt"
    "math"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceCurrencyService = "service-example-currency-service"
)

//melody:service ServiceCurrencyService
func NewCurrencyService(
    currencyRepository repository.CurrencyRepository,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
    clockInstance melodyclockcontract.Clock,
) *CurrencyService {
    return &CurrencyService{
        currencyRepository: currencyRepository,
        cache:              cacheInstance,
        eventDispatcher:    eventDispatcher,
        clock:              clockInstance,
    }
}

/* CurrencyService stamps the instant a rate was quoted with the injected clock rather than the wall, the
   rule ProductService states for its own timestamps: a frozen clock lets a test name the exact instant a
   currency carries, which cannot be written against time.Now. */
type CurrencyService struct {
    currencyRepository repository.CurrencyRepository
    cache              melodycachecontract.Cache
    eventDispatcher    melodyeventcontract.EventDispatcher
    clock              melodyclockcontract.Clock
}

/* the range a rate is admitted in. The lower bound is below the smallest real quote by three orders of
   magnitude and the upper one above the largest by the same, so no currency is refused for being cheap or
   dear; what they refuse is a number that is valid JSON and not a price — 1e308, which a conversion turns
   into an infinity the serializer cannot render, or a denormal that does the same from below. */
const (
    minUsableRate = 1e-6
    maxUsableRate = 1e9
)

/* ErrUnusableRate is the sentinel beneath every refusal of a rate: a caller sweeping a whole document reads
   it with errors.Is to tell a quote the catalogue refused — which it counts and goes past — from a backend
   that failed, which is not the provider's fault and stops the sweep. */
var ErrUnusableRate = errors.New("the exchange rate must be a positive, finite number within the range a quote can take")

/* refuseUnusableRate is the one spelling of the rule, read by both write doors. A conversion divides by the
   source rate, so a zero divides by zero and a negative flips the price's sign; an infinity or a value the
   range above excludes produces a converted price that is not a finite number, which the read door then
   cannot answer; and the column is NOT NULL, so there is no "not quoted yet" to fall back on — a currency
   enters the catalogue with a quote or it does not enter it. */
func refuseUnusableRate(currencyId string, rate float64) error {
    if true == isUsableRate(rate) {
        return nil
    }

    return exception.NewError(
        ErrUnusableRate.Error(),
        exceptioncontract.Context{
            "currencyId": currencyId,
            "rate":       contextNumber(rate),
            "minimum":    minUsableRate,
            "maximum":    maxUsableRate,
        },
        ErrUnusableRate,
    )
}

/* contextNumber is a float as an error context can carry it. encoding/json refuses NaN and both infinities,
   and a json journal handed one falls back to a single text rendering of the WHOLE context — every other
   key of the record loses its structure, in exactly the record that describes a number that was not one.
   A finite value is left a number; one that is not travels as its text, "NaN", "+Inf" or "-Inf". */
func contextNumber(value float64) any {
    if true == math.IsNaN(value) || true == math.IsInf(value, 0) {
        return strconv.FormatFloat(value, 'g', -1, 64)
    }

    return value
}

/* quoteInstantOf is the resolution a quote's instant is held at: the column is DATETIME(6), so the row
   comes back truncated to the microsecond, and an instant compared at the nanosecond against it was
   never Equal — a provider stamping time.Now() with nine decimals made every unchanged quote read as a
   full-row update that changed nothing, which the driver reports as no row and the sweep counted as the
   currency having vanished, with the cache entries the unchanged branch drops left standing. Both write
   doors hold the instant at this resolution, so the in-memory repository and the database agree. */
func quoteInstantOf(instant time.Time) time.Time {
    return instant.UTC().Truncate(time.Microsecond)
}

/* isUsableRate is the two comparisons alone: an infinity fails the upper one and a NaN fails both, so
   neither needs a check of its own that the comparisons would shadow */
func isUsableRate(rate float64) bool {
    return minUsableRate <= rate && rate <= maxUsableRate
}

func (instance *CurrencyService) List() ([]*entity.Currency, error) {
    currencies, rememberErr := melodycache.Remember(
        instance.cache,
        CacheKeyCurrencyList,
        0,
        func(ctx context.Context) (any, error) {
            return instance.currencyRepository.All(ctx)
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    typed, ok := currencies.([]*entity.Currency)
    if false == ok {
        return nil, fmt.Errorf("invalid cache value for currency list")
    }

    return typed, nil
}

func (instance *CurrencyService) FindById(id string) (*entity.Currency, bool, error) {
    /* an identifier the cache-key grammar refuses names a row no write door admits, so it is answered as absent instead of asked of a cache that would refuse the question with a 500 */
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyCurrencyById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        instance.cache,
        cacheKey,
        func(ctx context.Context) (any, error) {
            currency, found, findErr := instance.currencyRepository.FindById(ctx, id)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return currency, nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    currency, ok := cached.(*entity.Currency)
    if false == ok {
        return nil, false, fmt.Errorf("invalid cache value for currency")
    }

    return currency, true, nil
}

/* Create takes the rate because the schema holds one and a currency with no quote cannot be converted to or
   from. The instant stamped is the clock's, not a provider's: the caller supplying the number IS the
   reading, and the refresh overwrites both halves the moment it reaches this currency. */
func (instance *CurrencyService) Create(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    code string,
    name string,
    rate float64,
) (*entity.Currency, error) {
    if rateErr := refuseUnusableRate(currencyId, rate); nil != rateErr {
        return nil, rateErr
    }

    currency := entity.NewCurrency(currencyId, code, name, rate, quoteInstantOf(instance.clock.Now()))

    createErr := instance.currencyRepository.Create(runtimeInstance.Context(), currency)
    if nil != createErr {
        return nil, createErr
    }

    createdEvent := event.NewCurrencyCreatedEvent(currency)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyCreatedEventName,
        createdEvent,
    )
    if nil != dispatchErr {
        return nil, dispatchErr
    }

    return currency, nil
}

func (instance *CurrencyService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    code string,
    name string,
) (*entity.Currency, bool, error) {
    ctx := runtimeInstance.Context()

    currency, found, findErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    /* the loaded entity is the repository's own stored value under the in-memory configuration, shared with every concurrent reader, so the changes land on a copy: a refused update leaves the stored entity exactly as it was */
    modified := *currency
    modified.Code = code
    modified.Name = name

    updated, updateErr := instance.currencyRepository.Update(ctx, &modified)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    /* the rename writes the code and the name alone, and a refresh may have written the quote between the read
       above and the write: the entity answered and published is the row as it now stands, read back, not the
       copy of what was read before the write — which carried the quote the refresh had just replaced */
    written, stillFound, rereadErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != rereadErr {
        /* the row is written, and the caches that serve its old code and name expire never: the event goes out
           with the fields as written — the quote as it was read before the write — so they are dropped even
           though the row cannot be read back, and the read-back failure is what the caller is answered */
        _, dispatchErr := instance.eventDispatcher.DispatchName(
            runtimeInstance,
            event.CurrencyUpdatedEventName,
            event.NewCurrencyUpdatedEvent(&modified),
        )

        return nil, true, errors.Join(rereadErr, dispatchErr)
    }
    if false == stillFound {
        return nil, false, nil
    }

    updatedEvent := event.NewCurrencyUpdatedEvent(written)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return written, true, nil
}

/* RateUpdateOutcome is what UpdateRate did with a quote, in a word the caller can count under the right
   heading: written, or not written for one of three reasons that are not failures and are not the same —
   the currency stopped existing between the listing and its update, the quote is older than the one the
   catalogue already holds, or the quote is the one it already holds. The bool this replaced folded the last
   two into "absent" — on mysql a full-row UPDATE that changes nothing affects zero rows, which the door read
   as the row having vanished, so a provider whose quotes had not moved was reported as three deleted
   currencies every tick. */
type RateUpdateOutcome int

const (
    RateUpdateAbsent RateUpdateOutcome = iota
    RateUpdateStale
    RateUpdateUnchanged
    RateUpdateWritten
)

/* rateUpdateOutcomeName spells an outcome the way the refresh reports it, for the context of a failure that
   stopped the sweep: the word is the heading the same quote is counted under in the table. */
func rateUpdateOutcomeName(outcome RateUpdateOutcome) string {
    switch outcome {
    case RateUpdateWritten:
        return "written"
    case RateUpdateUnchanged:
        return "unchanged"
    case RateUpdateStale:
        return "stale"
    default:
        return "absent"
    }
}

/* UpdateRate is the door the rate refresh writes through, and it goes through the service rather than
   straight to the repository for one reason: the currency list and every currency by id are cached, and
   the listeners that drop those entries are subscribed to the updated event this dispatches. A rate written
   behind the cache is a rate no reader ever sees — in the process that dispatched, which is the refresh's
   own; the http server sees the drop through the shared cache alone, and on the in-process fallback it
   serves what it cached until it restarts.

   The rate is judged by refuseUnusableRate, the spelling Create reads too, and the refusal names the
   currency so a caller sweeping a whole document can say which quote was bad and go on to the next.

   The quote carries its instant in both reference frames — see entity.RateQuote. The reading the catalogue
   already holds — the same provider stamp at the same rate — is not written again, however this clock measured
   its arrival, and the cache entries of the currency that do not serve the row are dropped without an event:
   the write would change nothing and the event would journal a change that did not happen, while the drop is
   what heals a cache that kept a previous state after the invalidation of an earlier write failed. A reading
   older than the one stored, on THIS clock, is not written: a replay, or a stale cache in front of the
   provider, is older here even when the provider's clock was set back between the two, and never a newer
   price. The instants are judged at the microsecond the columns hold, so "the same instant" means what the
   row can say. An entry that already serves the row is kept — see healCachedCurrency. */
func (instance *CurrencyService) UpdateRate(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    quote entity.RateQuote,
) (*entity.Currency, RateUpdateOutcome, error) {
    if rateErr := refuseUnusableRate(currencyId, quote.Rate); nil != rateErr {
        return nil, RateUpdateAbsent, rateErr
    }

    ctx := runtimeInstance.Context()
    quote = entity.NewRateQuote(quote.Rate, quoteInstantOf(quote.AsOf), quoteInstantOf(quote.ProviderAsOf))

    currency, found, findErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != findErr {
        return nil, RateUpdateAbsent, findErr
    }

    if false == found {
        return nil, RateUpdateAbsent, nil
    }

    if true == quote.NamesTheSameReadingAs(currency.Quote()) {
        if dropErr := instance.healCachedCurrency(currency); nil != dropErr {
            return nil, RateUpdateUnchanged, dropErr
        }

        return currency, RateUpdateUnchanged, nil
    }

    if true == isOlderReading(quote, currency.Quote()) {
        return currency, RateUpdateStale, nil
    }

    /* the write is CONDITIONAL on the row's reading, in one statement: the read above judged a row as it was,
       and two processes on one schedule — the refresh takes no lock — each judged their document newer than
       that row and wrote whole, so the older document landed last. The repository writes only over a row
       that does not hold a newer reading; a refusal is read back to tell a row that moved from one that
       vanished. */
    written, updateErr := instance.currencyRepository.UpdateQuote(ctx, currencyId, quote)
    if nil != updateErr {
        return nil, RateUpdateAbsent, updateErr
    }

    if false == written {
        current, stillThere, rereadErr := instance.currencyRepository.FindById(ctx, currencyId)
        if nil != rereadErr {
            return nil, RateUpdateAbsent, rereadErr
        }

        if false == stillThere {
            return nil, RateUpdateAbsent, nil
        }

        if false == quote.NamesTheSameReadingAs(current.Quote()) && true == isOlderReading(quote, current.Quote()) {
            return current, RateUpdateStale, nil
        }

        if dropErr := instance.healCachedCurrency(current); nil != dropErr {
            return nil, RateUpdateUnchanged, dropErr
        }

        return current, RateUpdateUnchanged, nil
    }

    modified := *currency
    modified.Rate = quote.Rate
    modified.RateAsOf = quote.AsOf
    modified.ProviderRateAsOf = quote.ProviderAsOf

    updatedEvent := event.NewCurrencyUpdatedEvent(&modified)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, RateUpdateWritten, dispatchErr
    }

    return &modified, RateUpdateWritten, nil
}

/* isOlderReading answers whether a quote is a reading older than the one held, judged on this application's
   clock. A quote carrying the held reading's own stamp is never older: it is the provider re-quoting that
   reading, and the instant this clock gave it moves with every measurement of the offset. */
func isOlderReading(quote entity.RateQuote, held entity.RateQuote) bool {
    if true == quote.ProviderAsOf.Equal(held.ProviderAsOf) {
        return false
    }

    return true == quote.AsOf.Before(held.AsOf)
}

/* healCachedCurrency makes the two entries a currency is served from — the same two the updated listener
   drops — agree with a row that did not move, by the keys and without the event. Each entry is READ first and
   dropped only when it is not the row: an entry holding another rate, another instant, another code or name,
   an absence cached for a row that exists, a list that lacks the currency, or a payload the cache cannot hand
   back. The whole row is compared, not the quote alone, because the heal stands in for EVERY invalidation that
   may have failed before it — the rename's included, whose listener drops the same two keys — and an entry
   no key expires is otherwise served as it is until the next write. Dropped unconditionally, as it used to be,
   a catalogue that had not moved cost the server four reads from the database and four writes into the cache
   on every tick, twenty-four times a day, to heal a cache that was almost never wrong; the two reads here are
   what the heal costs now. An entry that is absent is left absent, since the next reader fills it from the
   row. */
func (instance *CurrencyService) healCachedCurrency(currency *entity.Currency) error {
    byIdKey := CacheKeyCurrencyById(currency.Id)

    cached, exists, getErr := instance.cache.Get(byIdKey)
    if nil != getErr || (true == exists && false == cachedCurrencyIsRow(cached, currency)) {
        if byIdErr := instance.cache.Delete(byIdKey); nil != byIdErr {
            return byIdErr
        }
    }

    cachedList, listExists, listErr := instance.cache.Get(CacheKeyCurrencyList)
    if nil != listErr || (true == listExists && false == cachedListCarriesRow(cachedList, currency)) {
        return instance.cache.Delete(CacheKeyCurrencyList)
    }

    return nil
}

/* cachedCurrencyIsRow answers whether a cached entry is the row, field for field, the instants at the
   resolution the row holds. */
func cachedCurrencyIsRow(cached any, currency *entity.Currency) bool {
    typed, isCurrency := cached.(*entity.Currency)
    if false == isCurrency || nil == typed {
        return false
    }

    return currency.Id == typed.Id &&
        currency.Code == typed.Code &&
        currency.Name == typed.Name &&
        currency.Rate == typed.Rate &&
        true == currency.RateAsOf.Equal(typed.RateAsOf) &&
        true == currency.ProviderRateAsOf.Equal(typed.ProviderRateAsOf)
}

/* cachedListCarriesRow answers whether a cached list carries the currency as the row holds it. */
func cachedListCarriesRow(cached any, currency *entity.Currency) bool {
    typed, isList := cached.([]*entity.Currency)
    if false == isList {
        return false
    }

    for _, listed := range typed {
        if nil != listed && currency.Id == listed.Id {
            return true == cachedCurrencyIsRow(listed, currency)
        }
    }

    return false
}

func (instance *CurrencyService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
) (bool, error) {
    deleted, deleteErr := instance.currencyRepository.DeleteById(runtimeInstance.Context(), currencyId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    deletedEvent := event.NewCurrencyDeletedEvent(currencyId)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyDeletedEventName,
        deletedEvent,
    )
    if nil != dispatchErr {
        return true, dispatchErr
    }

    return true, nil
}

func MustGetCurrencyService(resolver melodycontainercontract.Resolver) *CurrencyService {
    return melodycontainer.MustFromResolver[*CurrencyService](
        resolver,
        ServiceCurrencyService,
    )
}
