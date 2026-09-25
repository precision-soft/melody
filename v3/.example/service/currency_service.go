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

/* CurrencyService stamps the instant a rate is quoted with the injected clock rather than the wall, so a frozen clock names the exact instant a currency carries. */
type CurrencyService struct {
    currencyRepository repository.CurrencyRepository
    cache              melodycachecontract.Cache
    eventDispatcher    melodyeventcontract.EventDispatcher
    clock              melodyclockcontract.Clock
}

/* the range a rate is admitted in, three orders of magnitude past the smallest and the largest real quote: it refuses a number that is valid JSON and not a price, which a conversion would turn into an infinity from above or below */
const (
    minUsableRate = 1e-6
    maxUsableRate = 1e9
)

/* ErrUnusableRate is the sentinel beneath every refusal of a rate, so a caller sweeping a document tells a refused quote, which it counts and goes past, from a backend failure, which stops the sweep. */
var ErrUnusableRate = errors.New("the exchange rate must be a positive, finite number within the range a quote can take")

/* refuseUnusableRate is the one spelling of the rule both write doors read: a rate that is zero, negative, infinite or outside the admitted range makes a conversion that is not a finite price, and the column is NOT NULL, so a currency enters the catalogue with a usable quote or not at all. */
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

/* contextNumber is a float as an error context can carry it: encoding/json refuses NaN and both infinities, and a json journal would then render the whole context as text, so such a value travels as "NaN", "+Inf" or "-Inf". */
func contextNumber(value float64) any {
    if true == math.IsNaN(value) || true == math.IsInf(value, 0) {
        return strconv.FormatFloat(value, 'g', -1, 64)
    }

    return value
}

/* quoteInstantOf holds a quote's instant at the microsecond the DATETIME(6) column keeps, so an instant compared against a row read back is Equal when it names the same instant, and the in-memory repository and the database agree. */
func quoteInstantOf(instant time.Time) time.Time {
    return instant.UTC().Truncate(time.Microsecond)
}

/* isUsableRate is the two comparisons alone: an infinity fails the upper one and a NaN fails both */
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
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
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

/* Create takes the rate because the schema holds one and a currency with no quote cannot be converted. The instant is the clock's: the caller supplying the number is the reading, and the refresh overwrites both when it reaches this currency. */
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

    /* under the in-memory configuration the loaded entity is the repository's stored value, shared with concurrent readers, so the changes land on a copy and a refused update leaves the stored entity untouched */
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

    /* the rename writes the code and the name alone while a refresh may write the quote between the read and the write, so the entity answered and published is the row read back */
    written, stillFound, rereadErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != rereadErr {
        /* the row is written and the entries serving its old code and name never expire, so the event goes out with the fields as written even though the row cannot be read back, and the read-back failure is what the caller is answered */
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

/* RateUpdateOutcome is what UpdateRate did with a quote: written, or not written because the currency is gone, the quote is older than the one held, or it is the one held. None of the three is a failure, and each is counted under its own heading. */
type RateUpdateOutcome int

const (
    RateUpdateAbsent RateUpdateOutcome = iota
    RateUpdateStale
    RateUpdateUnchanged
    RateUpdateWritten
)

/* rateUpdateOutcomeName spells an outcome as the refresh reports it, the heading the quote is counted under in the table. */
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

/* UpdateRate is the door the rate refresh writes through. It goes through the service because the currency list and every currency by id are cached, and the listeners that drop those entries subscribe to the updated event it dispatches in the process that dispatched; the http server sees the drop through the shared cache alone. The rate is judged by refuseUnusableRate. The reading already held, the same provider stamp at the same rate, is not written again: the entries that do not serve the row are dropped without an event (see healCachedCurrency). A reading older than the one held on this clock is not written, and instants are compared at the microsecond the columns hold. */
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

    /* the write is conditional on the row's reading, in one statement: the refresh takes no lock, so two processes on one schedule may each judge their document newer, and the repository writes only over a row that holds no newer reading; a refusal is read back to tell a row that moved from one that vanished */
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

/* isOlderReading answers whether a quote is older than the reading held, judged on this application's clock. A quote carrying the held reading's own stamp is never older: it is the provider re-quoting that reading. */
func isOlderReading(quote entity.RateQuote, held entity.RateQuote) bool {
    if true == quote.ProviderAsOf.Equal(held.ProviderAsOf) {
        return false
    }

    return true == quote.AsOf.Before(held.AsOf)
}

/* healCachedCurrency makes the two entries a currency is served from, the same two the updated listener drops, agree with a row that did not move, without the event. Each entry is read first and dropped only when it is not the row, compared field for field, because the heal stands in for any invalidation that failed before it, the rename's included. An absent entry is left absent: the next reader fills it from the row. */
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
