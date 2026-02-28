package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/.example/domain/entity"
    "github.com/precision-soft/melody/.example/domain/event"
    "github.com/precision-soft/melody/.example/domain/repository"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    ServiceCategoryService = "service-example-category-service"
)

func NewCategoryService(
    categoryRepository repository.CategoryRepository,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
) *CategoryService {
    return &CategoryService{
        categoryRepository: categoryRepository,
        cache:              cacheInstance,
        eventDispatcher:    eventDispatcher,
    }
}

type CategoryService struct {
    categoryRepository repository.CategoryRepository
    cache              melodycachecontract.Cache
    eventDispatcher    melodyeventcontract.EventDispatcher
}

func (instance *CategoryService) List() ([]*entity.Category, error) {
    categories, rememberErr := melodycache.Remember(
        instance.cache,
        CacheKeyCategoryList,
        0,
        func(ctx context.Context) (any, error) {
            return instance.categoryRepository.All(), nil
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    typed, ok := categories.([]*entity.Category)
    if false == ok {
        return nil, fmt.Errorf("invalid cache value for category list")
    }

    return typed, nil
}

func (instance *CategoryService) FindById(id string) (*entity.Category, bool, error) {
    cacheKey := CacheKeyCategoryById(id)

    cached, rememberErr := melodycache.Remember(
        instance.cache,
        cacheKey,
        0,
        func(ctx context.Context) (any, error) {
            category, found := instance.categoryRepository.FindById(id)
            if false == found {
                return nil, nil
            }

            return category, nil
        },
        nil,
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    category, ok := cached.(*entity.Category)
    if false == ok {
        return nil, false, fmt.Errorf("invalid cache value for category")
    }

    return category, true, nil
}

func (instance *CategoryService) Create(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
    name string,
) (*entity.Category, error) {
    category := entity.NewCategory(categoryId, name)

    createErr := instance.categoryRepository.Create(category)
    if nil != createErr {
        return nil, createErr
    }

    createdEvent := event.NewCategoryCreatedEvent(category)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CategoryCreatedEventName,
        createdEvent,
    )
    if nil != dispatchErr {
        return nil, dispatchErr
    }

    return category, nil
}

func (instance *CategoryService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
    name string,
) (*entity.Category, bool, error) {
    category, found := instance.categoryRepository.FindById(categoryId)
    if false == found {
        return nil, false, nil
    }

    category.Name = name

    updated, updateErr := instance.categoryRepository.Update(category)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    updatedEvent := event.NewCategoryUpdatedEvent(category)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CategoryUpdatedEventName,
        updatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return category, true, nil
}

func (instance *CategoryService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
) (bool, error) {
    deleted, deleteErr := instance.categoryRepository.DeleteById(categoryId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    deletedEvent := event.NewCategoryDeletedEvent(categoryId)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.CategoryDeletedEventName,
        deletedEvent,
    )
    if nil != dispatchErr {
        return true, dispatchErr
    }

    return true, nil
}

func MustGetCategoryService(resolver melodycontainercontract.Resolver) *CategoryService {
    return melodycontainer.MustFromResolver[*CategoryService](
        resolver,
        ServiceCategoryService,
    )
}
