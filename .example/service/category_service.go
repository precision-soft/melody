package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/event"
    "github.com/precision-soft/melody/.example/repository"
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
            return instance.categoryRepository.All(ctx)
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
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyCategoryById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        instance.cache,
        cacheKey,
        func(ctx context.Context) (any, error) {
            category, found, findErr := instance.categoryRepository.FindById(ctx, id)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return category, nil
        },
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

    createErr := instance.categoryRepository.Create(runtimeInstance.Context(), category)
    if nil != createErr {
        return nil, createErr
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.CategoryCreatedEventName, event.NewCategoryCreatedEvent(category), category.Id, CacheKeyCategoryList, CacheKeyCategoryById(category.Id))

    return category, nil
}

func (instance *CategoryService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
    name string,
) (*entity.Category, bool, error) {
    ctx := runtimeInstance.Context()

    category, found, findErr := instance.categoryRepository.FindById(ctx, categoryId)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    category.Name = name

    updated, updateErr := instance.categoryRepository.Update(ctx, category)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.CategoryUpdatedEventName, event.NewCategoryUpdatedEvent(category), category.Id, CacheKeyCategoryList, CacheKeyCategoryById(category.Id))

    return category, true, nil
}

func (instance *CategoryService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
) (bool, error) {
    deleted, deleteErr := instance.categoryRepository.DeleteById(runtimeInstance.Context(), categoryId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.CategoryDeletedEventName, event.NewCategoryDeletedEvent(categoryId), categoryId, CacheKeyCategoryList, CacheKeyCategoryById(categoryId))

    return true, nil
}

func MustGetCategoryService(resolver melodycontainercontract.Resolver) *CategoryService {
    return melodycontainer.MustFromResolver[*CategoryService](
        resolver,
        ServiceCategoryService,
    )
}
