package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceCategoryService = "service-example-category-service"
)

//melody:service ServiceCategoryService
func NewCategoryService(
    categoryRepository repository.CategoryRepository,
    productRepository repository.ProductRepository,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
) *CategoryService {
    return &CategoryService{
        categoryRepository: categoryRepository,
        productRepository:  productRepository,
        cache:              cacheInstance,
        eventDispatcher:    eventDispatcher,
    }
}

type CategoryService struct {
    categoryRepository repository.CategoryRepository
    productRepository  repository.ProductRepository
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
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
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

            /* the database compares the identifier under its collation, which pads trailing spaces, so a row found for another spelling is answered absent: cached under the spelling asked, it would be a copy no listener drops */
            if false == found || id != category.Id {
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

    /* under the in-memory configuration the loaded entity is the repository's stored value, shared with concurrent readers, so the change lands on a copy and a refused update leaves the stored entity untouched */
    modified := *category
    modified.Name = name

    updated, updateErr := instance.categoryRepository.Update(ctx, &modified)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.CategoryUpdatedEventName, event.NewCategoryUpdatedEvent(&modified), modified.Id, CacheKeyCategoryList, CacheKeyCategoryById(modified.Id))

    return &modified, true, nil
}

func (instance *CategoryService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    categoryId string,
) (bool, error) {
    deleted := false

    /* the product table's foreign key refuses the delete on the database; the read answers the same refusal on the configuration without one, held with the delete against a concurrent product write naming the category */
    deleteErr := instance.productRepository.HoldingReferences(func() error {
        categorizedIn, categorizedInErr := instance.productRepository.CategorizedIn(runtimeInstance.Context(), categoryId)
        if nil != categorizedInErr {
            return categorizedInErr
        }

        if true == categorizedIn {
            return repository.ErrCategoryInUse
        }

        var removeErr error
        deleted, removeErr = instance.categoryRepository.DeleteById(runtimeInstance.Context(), categoryId)

        return removeErr
    })
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
