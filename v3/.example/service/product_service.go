package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceProductService = "service-example-product-service"
)

//melody:service ServiceProductService
func NewProductService(
    productRepository repository.ProductRepository,
    categoryService *CategoryService,
    currencyService *CurrencyService,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
    clockInstance melodyclockcontract.Clock,
) *ProductService {
    return &ProductService{
        productRepository: productRepository,
        categoryService:   categoryService,
        currencyService:   currencyService,
        cache:             cacheInstance,
        eventDispatcher:   eventDispatcher,
        clock:             clockInstance,
    }
}

/* ProductService stamps every write with the injected clock rather than the wall, so a frozen clock names the exact instant a product carries. */
type ProductService struct {
    productRepository repository.ProductRepository
    categoryService   *CategoryService
    currencyService   *CurrencyService
    cache             melodycachecontract.Cache
    eventDispatcher   melodyeventcontract.EventDispatcher
    clock             melodyclockcontract.Clock
}

func (instance *ProductService) List() ([]*entity.Product, error) {
    products, rememberErr := cache.Remember(
        instance.cache,
        CacheKeyProductList,
        0,
        func(ctx context.Context) (any, error) {
            return instance.productRepository.All(ctx)
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    typed, ok := products.([]*entity.Product)
    if false == ok {
        return nil, fmt.Errorf("invalid cache value for product list")
    }

    return typed, nil
}

func (instance *ProductService) FindById(id string) (*entity.Product, bool, error) {
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyProductById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        instance.cache,
        cacheKey,
        func(ctx context.Context) (any, error) {
            product, found, findErr := instance.productRepository.FindById(ctx, id)
            if nil != findErr {
                return nil, findErr
            }

            /* the database compares the identifier under its collation, which pads trailing spaces, so a row found for another spelling is answered absent: cached under the spelling asked, it would be a copy no listener drops */
            if false == found || id != product.Id {
                return nil, nil
            }

            return product, nil
        },
    )
    if nil != rememberErr {
        return nil, false, rememberErr
    }

    if nil == cached {
        return nil, false, nil
    }

    product, ok := cached.(*entity.Product)
    if false == ok {
        return nil, false, fmt.Errorf("invalid cache value for product")
    }

    return product, true, nil
}

/* refuseUnknownReferences answers the refusal of a write whose category or currency names nothing, read from the repositories rather than the caches. The product table's foreign keys refuse the same write on the database, against a delete that lands between this read and the write; on the configuration without one this read is the check, held with the write under HoldingReferences. */
func (instance *ProductService) refuseUnknownReferences(ctx context.Context, categoryId string, currencyId string) error {
    if _, found, findErr := instance.categoryService.categoryRepository.FindById(ctx, categoryId); nil != findErr || false == found {
        if nil != findErr {
            return findErr
        }

        return repository.ErrUnknownCategory
    }

    if _, found, findErr := instance.currencyService.currencyRepository.FindById(ctx, currencyId); nil != findErr || false == found {
        if nil != findErr {
            return findErr
        }

        return repository.ErrUnknownCurrency
    }

    return nil
}

/* RecordView counts one read of a product and answers the count so far. The counter is the cache backend's atomic increment, shared by every process on the shared cache; it is a hint rather than a ledger, since example:cache:clear and example:db:reset empty the namespace it lives in. */
func (instance *ProductService) RecordView(id string) (int64, error) {
    return instance.cache.Increment(CacheKeyProductViews(id), 1)
}

func (instance *ProductService) Create(
    runtimeInstance melodyruntimecontract.Runtime,
    productId string,
    name string,
    description string,
    categoryId string,
    price float64,
    currencyId string,
    stock int64,
) (*entity.Product, error) {
    now := instance.clock.Now()
    product := entity.NewProduct(
        productId,
        name,
        description,
        categoryId,
        price,
        currencyId,
        stock,
        now,
        now,
    )

    /* the reference check and the write are one step against a concurrent delete of what the product names */
    createErr := instance.productRepository.HoldingReferences(func() error {
        if referenceErr := instance.refuseUnknownReferences(WriteContext(runtimeInstance), categoryId, currencyId); nil != referenceErr {
            return referenceErr
        }

        return instance.productRepository.Create(WriteContext(runtimeInstance), product)
    })
    if nil != createErr {
        return nil, createErr
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.ProductCreatedEventName, event.NewProductCreatedEvent(product), product.Id, CacheKeyProductList, CacheKeyProductById(product.Id))

    return product, nil
}

func (instance *ProductService) Update(
    runtimeInstance melodyruntimecontract.Runtime,
    id string,
    name string,
    description string,
    categoryId string,
    price float64,
    currencyId string,
    stock int64,
) (*entity.Product, bool, error) {
    ctx := WriteContext(runtimeInstance)

    product, found, findErr := instance.productRepository.FindById(ctx, id)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    /* under the in-memory configuration the loaded entity is the repository's stored value, shared with concurrent readers, so the changes land on a copy: a refused update leaves it untouched and no reader sees it half-written */
    modified := *product
    modified.Name = name
    modified.Description = description
    modified.CategoryId = categoryId
    modified.Price = price
    modified.CurrencyId = currencyId
    modified.Stock = stock
    modified.UpdatedAt = instance.clock.Now()

    updated := false

    /* the reference check and the write are one step against a concurrent delete of what the product names */
    updateErr := instance.productRepository.HoldingReferences(func() error {
        if referenceErr := instance.refuseUnknownReferences(ctx, categoryId, currencyId); nil != referenceErr {
            return referenceErr
        }

        var writeErr error
        updated, writeErr = instance.productRepository.Update(ctx, &modified)

        return writeErr
    })
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.ProductUpdatedEventName, event.NewProductUpdatedEvent(&modified), modified.Id, CacheKeyProductList, CacheKeyProductById(modified.Id))

    return &modified, true, nil
}

func (instance *ProductService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    productId string,
) (bool, error) {
    deleted, deleteErr := instance.productRepository.DeleteById(WriteContext(runtimeInstance), productId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.ProductDeletedEventName, event.NewProductDeletedEvent(productId), productId, CacheKeyProductList, CacheKeyProductById(productId))

    return true, nil
}

func MustGetProductService(resolver melodycontainercontract.Resolver) *ProductService {
    return container.MustFromResolver[*ProductService](
        resolver,
        ServiceProductService,
    )
}
