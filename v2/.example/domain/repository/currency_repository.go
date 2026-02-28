package repository

import (
    "github.com/precision-soft/melody/v2/.example/domain/entity"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

const (
    ServiceCurrencyRepository = "service.example.currency.repository"
)

type CurrencyRepository interface {
    All() []*entity.Currency

    FindById(id string) (*entity.Currency, bool)

    Create(currency *entity.Currency) error

    Update(currency *entity.Currency) (bool, error)

    DeleteById(id string) (bool, error)
}

func MustGetCurrencyRepository(resolver melodycontainercontract.Resolver) CurrencyRepository {
    return melodycontainer.MustFromResolver[CurrencyRepository](resolver, ServiceCurrencyRepository)
}
