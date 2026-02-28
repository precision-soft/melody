package repository

import (
    "github.com/precision-soft/melody/v2/.example/domain/entity"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
)

const (
    ServiceProductRepository = "service.example.product.repository"
)

type ProductRepository interface {
    All() []*entity.Product

    FindById(id string) (*entity.Product, bool)

    Create(product *entity.Product) error

    Update(product *entity.Product) (bool, error)

    DeleteById(id string) (bool, error)
}

func MustGetProductRepository(resolver melodycontainercontract.Resolver) ProductRepository {
    return melodycontainer.MustFromResolver[ProductRepository](resolver, ServiceProductRepository)
}
