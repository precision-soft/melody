package service

import (
    "context"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    ServiceCategoryService = "service.example.category_service"
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

func (instance *CategoryService) List(runtimeInstance melodycontainercontract.Runtime) ([]*entity.Category, error) {
    categories, rememberErr := melodycache.Remember(
        melodycache.For(runtimeInstance, instance.cache),
        CacheKeyCategoryList,
        melodycache.RememberConfig{
            Ttl: melodybound.Of(entityCacheTtl),
        },
        func(flightRuntime melodycontainercontract.Runtime, _ *melodycache.RememberItem) ([]*entity.Category, error) {
            return instance.categoryRepository.All(flightRuntime.Context())
        },
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    return categories, nil
}

func (instance *CategoryService) FindById(runtimeInstance melodycontainercontract.Runtime, id string) (*entity.Category, bool, error) {
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyCategoryById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        melodycache.For(runtimeInstance, instance.cache),
        cacheKey,
        func(flightRuntime melodycontainercontract.Runtime) (*entity.Category, error) {
            category, found, findErr := instance.categoryRepository.FindById(flightRuntime.Context(), id)
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

    return cached, true, nil
}

func (instance *CategoryService) Create(
    runtimeInstance melodycontainercontract.Runtime,
    categoryId string,
    name string,
) (*entity.Category, error) {
    category := entity.NewCategory(categoryId, name)

    createErr := instance.categoryRepository.Create(runtimeInstance.Context(), category)
    if nil != createErr {
        return nil, createErr
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventCategoryCreated, event.NewCategoryCreatedEvent(category), category.Id, CacheKeyCategoryList, CacheKeyCategoryById(category.Id))

    return category, nil
}

func (instance *CategoryService) Update(
    runtimeInstance melodycontainercontract.Runtime,
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

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventCategoryUpdated, event.NewCategoryUpdatedEvent(&modified), modified.Id, CacheKeyCategoryList, CacheKeyCategoryById(modified.Id))

    return &modified, true, nil
}

func (instance *CategoryService) DeleteById(
    runtimeInstance melodycontainercontract.Runtime,
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

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventCategoryDeleted, event.NewCategoryDeletedEvent(categoryId), categoryId, CacheKeyCategoryList, CacheKeyCategoryById(categoryId))

    return true, nil
}

func MustGetCategoryService(ctx context.Context, resolver melodycontainercontract.Resolver) *CategoryService {
    return melodycontainer.MustFromResolver[*CategoryService](
        ctx,
        resolver,
        ServiceCategoryService,
    )
}
