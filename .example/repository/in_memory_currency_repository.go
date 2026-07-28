package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"

    "github.com/precision-soft/melody/.example/entity"
)

func NewInMemoryCurrencyRepository() CurrencyRepository {
    return &inMemoryCurrencyRepository{currencies: seedCurrencyList()}
}

type inMemoryCurrencyRepository struct {
    mutex      sync.RWMutex
    currencies []*entity.Currency
}

/* @info the returned slice is a copy, but a shallow one: the entity pointers stay shared with the
repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.Currency{}, instance.currencies...), nil
}

func (instance *inMemoryCurrencyRepository) FindById(ctx context.Context, id string) (*entity.Currency, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    currency, found := instance.findByIdLocked(id)

    return currency, found, nil
}

func (instance *inMemoryCurrencyRepository) findByIdLocked(id string) (*entity.Currency, bool) {
    for _, currency := range instance.currencies {
        if nil == currency {
            continue
        }

        if id == currency.Id {
            return currency, true
        }
    }

    return nil, false
}

func (instance *inMemoryCurrencyRepository) Create(ctx context.Context, currency *entity.Currency) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateCurrency(currency)
    if nil != validationErr {
        return validationErr
    }

    if "" == strings.TrimSpace(currency.Id) {
        currency.Id = nextCurrencyId(instance.identifierListLocked())
    }

    _, exists := instance.findByIdLocked(currency.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    instance.currencies = append(instance.currencies, currency)
    return nil
}

func (instance *inMemoryCurrencyRepository) Update(ctx context.Context, currency *entity.Currency) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    validationErr := validateCurrency(currency)
    if nil != validationErr {
        return false, validationErr
    }

    id := strings.TrimSpace(currency.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    for index, existing := range instance.currencies {
        if nil == existing {
            continue
        }

        if id != existing.Id {
            continue
        }

        instance.currencies[index] = currency
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryCurrencyRepository) DeleteById(ctx context.Context, id string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    normalizedId := strings.TrimSpace(id)
    if "" == normalizedId {
        return false, fmt.Errorf("id is required")
    }

    for index, currency := range instance.currencies {
        if nil == currency {
            continue
        }

        if normalizedId != currency.Id {
            continue
        }

        instance.currencies = append(instance.currencies[:index], instance.currencies[index+1:]...)
        return true, nil
    }

    return false, nil
}

func (instance *inMemoryCurrencyRepository) identifierListLocked() []string {
    identifierList := make([]string, 0, len(instance.currencies))

    for _, currency := range instance.currencies {
        if nil == currency {
            continue
        }

        identifierList = append(identifierList, currency.Id)
    }

    return identifierList
}

var _ CurrencyRepository = (*inMemoryCurrencyRepository)(nil)
