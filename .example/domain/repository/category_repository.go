package repository

import (
    "github.com/precision-soft/melody/.example/domain/entity"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

const (
    ServiceCategoryRepository = "service.example.category.repository"
)

type CategoryRepository interface {
    All() []*entity.Category

    FindById(id string) (*entity.Category, bool)

    Create(category *entity.Category) error

    Update(category *entity.Category) (bool, error)

    DeleteById(id string) (bool, error)
}

func MustGetCategoryRepository(resolver melodycontainercontract.Resolver) CategoryRepository {
    return melodycontainer.MustFromResolver[CategoryRepository](resolver, ServiceCategoryRepository)
}
