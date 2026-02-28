package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/event"
    "github.com/precision-soft/melody/v2/.example/domain/repository"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const (
    ServiceCurrencyService = "service-example-currency-service"
)

func NewCurrencyService(
    currencyRepository repository.CurrencyRepository,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
) *CurrencyService {
    return &CurrencyService{
        currencyRepository: currencyRepository,
        cache:              cacheInstance,
        eventDispatcher:    eventDispatcher,
    }
}

type CurrencyService struct {
    currencyRepository repository.CurrencyRepository
    cache              melodycachecontract.Cache
    eventDispatcher    melodyeventcontract.EventDispatcher
}

func (instance *CurrencyService) List() ([]*entity.Currency, error) {
    currencies, rememberErr := melodycache.Remember(
        instance.cache,
        CacheKeyCurrencyList,
        0,
        func(ctx context.Context) (any, error) {
            return instance.currencyRepository.All(), nil
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
    cacheKey := CacheKeyCurrencyById(id)

    cached, rememberErr := melodycache.Remember(
        instance.cache,
        cacheKey,
        0,
        func(ctx context.Context) (any, error) {
            currency, found := instance.currencyRepository.FindById(id)
            if false == found {
                return nil, nil
            }

            return currency, nil
        },
        nil,
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

func (instance *CurrencyService) Create(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
    code string,
    name string,
) (*entity.Currency, error) {
    currency := entity.NewCurrency(currencyId, code, name)

    createErr := instance.currencyRepository.Create(currency)
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
    currency, found := instance.currencyRepository.FindById(currencyId)
    if false == found {
        return nil, false, nil
    }

    currency.Code = code
    currency.Name = name

    updated, updateErr := instance.currencyRepository.Update(currency)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    updatedEvent := event.NewCurrencyUpdatedEvent(currency)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CurrencyUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return currency, true, nil
}

func (instance *CurrencyService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    currencyId string,
) (bool, error) {
    deleted, deleteErr := instance.currencyRepository.DeleteById(currencyId)
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
