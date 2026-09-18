package service

import (
    "context"
    "errors"
    "fmt"
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
            "rate":       rate,
            "minimum":    minUsableRate,
            "maximum":    maxUsableRate,
        },
        ErrUnusableRate,
    )
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

    updatedEvent := event.NewCurrencyUpdatedEvent(&modified)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return &modified, true, nil
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

   A quote older than the instant the catalogue holds is not written: the provider's document is a reading
   taken at rateAsOf, and a reading older than the one already stored is a replay or a stale cache in
   front of the provider, never a newer price. A quote equal to the stored one, at the same instant, is
   not written either, and the cache entries of the currency are dropped without an event: the write would
   change nothing and the event would journal a change that did not happen, while the drop is what heals a
   cache that kept the previous rate after the invalidation of an earlier tick failed. The instant is judged
   at the microsecond the column holds, so "the same instant" means what the row can say. The drop costs
   one delete of the list entry per unchanged currency, one delete too many for a sweep — a cost accepted
   over a door that would have to know it is being swept. */
func (instance *CurrencyService) UpdateRate(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    rate float64,
    rateAsOf time.Time,
) (*entity.Currency, RateUpdateOutcome, error) {
    if rateErr := refuseUnusableRate(currencyId, rate); nil != rateErr {
        return nil, RateUpdateAbsent, rateErr
    }

    ctx := runtimeInstance.Context()
    rateAsOf = quoteInstantOf(rateAsOf)

    currency, found, findErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != findErr {
        return nil, RateUpdateAbsent, findErr
    }

    if false == found {
        return nil, RateUpdateAbsent, nil
    }

    if true == rateAsOf.Before(currency.RateAsOf) {
        return currency, RateUpdateStale, nil
    }

    if rate == currency.Rate && true == rateAsOf.Equal(currency.RateAsOf) {
        if dropErr := instance.dropCachedCurrency(currencyId); nil != dropErr {
            return nil, RateUpdateUnchanged, dropErr
        }

        return currency, RateUpdateUnchanged, nil
    }

    /* the write is CONDITIONAL on the row's instant, in one statement: the read above judged a row as it was,
       and two processes on one schedule — the refresh takes no lock — each judged their document newer than
       that row and wrote whole, so the older document landed last. The repository writes only over a row
       that is not newer; a refusal is read back to tell a row that moved from one that vanished. */
    written, updateErr := instance.currencyRepository.UpdateQuote(ctx, currencyId, rate, rateAsOf)
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

        if true == current.RateAsOf.After(rateAsOf) {
            return current, RateUpdateStale, nil
        }

        if dropErr := instance.dropCachedCurrency(currencyId); nil != dropErr {
            return nil, RateUpdateUnchanged, dropErr
        }

        return current, RateUpdateUnchanged, nil
    }

    modified := *currency
    modified.Rate = rate
    modified.RateAsOf = rateAsOf

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

/* dropCachedCurrency drops the two entries a currency is served from, the same two the updated listener
   drops — by the keys, without the event, for the unchanged quote whose only job is to make sure the cache
   agrees with a row that did not move. */
func (instance *CurrencyService) dropCachedCurrency(currencyId string) error {
    if byIdErr := instance.cache.Delete(CacheKeyCurrencyById(currencyId)); nil != byIdErr {
        return byIdErr
    }

    return instance.cache.Delete(CacheKeyCurrencyList)
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
