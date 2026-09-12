package service

import (
    "context"
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

/* refuseNonPositiveRate is the one spelling of the rule, read by both write doors. A conversion divides by
   the source rate, so a zero divides by zero and a negative flips the price's sign; and the column is NOT
   NULL, so there is no "not quoted yet" to fall back on — a currency enters the catalogue with a quote or
   it does not enter it. */
func refuseNonPositiveRate(currencyId string, rate float64) error {
    if 0 < rate {
        return nil
    }

    return exception.NewError(
        "the exchange rate must be positive",
        exceptioncontract.Context{
            "currencyId": currencyId,
            "rate":       rate,
        },
        nil,
    )
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
    if rateErr := refuseNonPositiveRate(currencyId, rate); nil != rateErr {
        return nil, rateErr
    }

    currency := entity.NewCurrency(currencyId, code, name, rate, instance.clock.Now())

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

/* UpdateRate is the door the rate refresh writes through, and it goes through the service rather than
   straight to the repository for one reason: the currency list and every currency by id are cached, and
   the listeners that drop those entries are subscribed to the updated event this dispatches. A rate written
   behind the cache is a rate no reader ever sees.

   The rate is judged by refuseNonPositiveRate, the spelling Create reads too, and the refusal names the
   currency so a caller sweeping a whole document can say which quote was bad. */
func (instance *CurrencyService) UpdateRate(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    rate float64,
    rateAsOf time.Time,
) (*entity.Currency, bool, error) {
    if rateErr := refuseNonPositiveRate(currencyId, rate); nil != rateErr {
        return nil, false, rateErr
    }

    ctx := runtimeInstance.Context()

    currency, found, findErr := instance.currencyRepository.FindById(ctx, currencyId)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    /* the loaded entity is the repository's own stored value under the in-memory configuration, shared with every concurrent reader, so the changes land on a copy */
    modified := *currency
    modified.Rate = rate
    modified.RateAsOf = rateAsOf

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
