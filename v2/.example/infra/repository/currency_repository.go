package inmemoryrepository

import (
    "fmt"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v2/.example/domain/entity"
    "github.com/precision-soft/melody/v2/.example/domain/repository"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

func NewInMemoryCurrencyRepository() repository.CurrencyRepository {
    return &inMemoryCurrencyRepository{
        currencies: []*entity.Currency{
            entity.NewCurrency("cur-eur", "EUR", "Euro"),
            entity.NewCurrency("cur-usd", "USD", "US Dollar"),
            entity.NewCurrency("cur-ron", "RON", "Romanian Leu"),
        },
    }
}

type inMemoryCurrencyRepository struct {
    currencies []*entity.Currency
}

func (instance *inMemoryCurrencyRepository) All() []*entity.Currency {
    return instance.currencies
}

func (instance *inMemoryCurrencyRepository) FindById(id string) (*entity.Currency, bool) {
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
        currency.Id = instance.nextId()
    }

    _, exists := instance.FindById(currency.Id)
    if true == exists {
        return fmt.Errorf("id already exists")
    }

    instance.currencies = append(instance.currencies, currency)
    return nil
}

func (instance *inMemoryCurrencyRepository) Update(currency *entity.Currency) (bool, error) {
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

func (instance *inMemoryCurrencyRepository) nextId() string {
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

var _ repository.CurrencyRepository = (*inMemoryCurrencyRepository)(nil)

func CurrencyRepositoryProvider() melodycontainercontract.Provider[repository.CurrencyRepository] {
    return func(resolver melodycontainercontract.Resolver) (repository.CurrencyRepository, error) {
        return NewInMemoryCurrencyRepository(), nil
    }
}
