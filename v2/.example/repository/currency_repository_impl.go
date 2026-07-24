package repository

import (
    "fmt"
    "strconv"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v2/.example/entity"

    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

func NewInMemoryCurrencyRepository() CurrencyRepository {
    return &inMemoryCurrencyRepository{
        currencies: []*entity.Currency{
            entity.NewCurrency("cur-eur", "EUR", "Euro"),
            entity.NewCurrency("cur-usd", "USD", "US Dollar"),
            entity.NewCurrency("cur-ron", "RON", "Romanian Leu"),
        },
    }
}

type inMemoryCurrencyRepository struct {
    mutex      sync.RWMutex
    currencies []*entity.Currency
}

/* @info the returned slice is a copy, but a shallow one: the entity pointers stay shared with the
repository, so a caller that mutates an entity in place bypasses the lock */
func (instance *inMemoryCurrencyRepository) All() []*entity.Currency {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return append([]*entity.Currency{}, instance.currencies...)
}

func (instance *inMemoryCurrencyRepository) FindById(id string) (*entity.Currency, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.findByIdLocked(id)
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

func (instance *inMemoryCurrencyRepository) Create(currency *entity.Currency) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == currency {
        return fmt.Errorf("currency is required")
    }

    if "" == strings.TrimSpace(currency.Code) {
        return fmt.Errorf("code is required")
    }

    if "" == strings.TrimSpace(currency.Name) {
        return fmt.Errorf("name is required")
    }

    if "" == strings.TrimSpace(currency.Id) {
        currency.Id = instance.nextIdLocked()
    }

    _, exists := instance.findByIdLocked(currency.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    instance.currencies = append(instance.currencies, currency)
    return nil
}

func (instance *inMemoryCurrencyRepository) Update(currency *entity.Currency) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == currency {
        return false, fmt.Errorf("currency is required")
    }

    id := strings.TrimSpace(currency.Id)
    if "" == id {
        return false, fmt.Errorf("id is required")
    }

    if "" == strings.TrimSpace(currency.Code) {
        return false, fmt.Errorf("code is required")
    }

    if "" == strings.TrimSpace(currency.Name) {
        return false, fmt.Errorf("name is required")
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

func (instance *inMemoryCurrencyRepository) DeleteById(id string) (bool, error) {
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

func (instance *inMemoryCurrencyRepository) nextIdLocked() string {
    maxSuffix := int64(0)

    for _, currency := range instance.currencies {
        if nil == currency {
            continue
        }

        id := strings.TrimSpace(currency.Id)
        if false == strings.HasPrefix(id, "cur-") {
            continue
        }

        suffixString := strings.TrimPrefix(id, "cur-")
        parsedSuffix, parseErr := strconv.ParseInt(suffixString, 10, 64)
        if nil != parseErr {
            continue
        }

        if parsedSuffix > maxSuffix {
            maxSuffix = parsedSuffix
        }
    }

    return fmt.Sprintf("cur-%d", maxSuffix+1)
}

var _ CurrencyRepository = (*inMemoryCurrencyRepository)(nil)

func CurrencyRepositoryProvider() melodycontainercontract.Provider[CurrencyRepository] {
    return func(resolver melodycontainercontract.Resolver) (CurrencyRepository, error) {
        return NewInMemoryCurrencyRepository(), nil
    }
}
