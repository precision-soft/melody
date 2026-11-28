package service

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    ServiceProductService = "service.example.product_service"
)

//melody:service ServiceProductService
func NewProductService(
    productRepository repository.ProductRepository,
    categoryService *CategoryService,
    currencyService *CurrencyService,
    cacheInstance melodycachecontract.Cache,
    eventDispatcher melodyeventcontract.EventDispatcher,
    clockInstance melodyclockcontract.Clock,
    productViewsWindow time.Duration,
) *ProductService {
    return &ProductService{
        productRepository:  productRepository,
        categoryService:    categoryService,
        currencyService:    currencyService,
        cache:              cacheInstance,
        eventDispatcher:    eventDispatcher,
        clock:              clockInstance,
        productViewsWindow: productViewsWindow,
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

    productViewsWindow time.Duration
}

/* ProductViews is a product's two view counters: Total since the product was first read, Daily since the first read of the current window, which lasts ViewsWindow from that read. */
type ProductViews struct {
    Total int64
    Daily int64
}

func (instance *ProductService) List(runtimeInstance melodycontainercontract.Runtime) ([]*entity.Product, error) {
    products, rememberErr := cache.Remember(
        cache.For(runtimeInstance, instance.cache),
        CacheKeyProductList,
        cache.RememberOptions{
            Ttl: bound.Of(entityCacheTtl),
        },
        func(flightRuntime melodycontainercontract.Runtime, _ *cache.RememberItem) ([]*entity.Product, error) {
            return instance.productRepository.All(flightRuntime.Context())
        },
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    return products, nil
}

func (instance *ProductService) FindById(runtimeInstance melodycontainercontract.Runtime, id string) (*entity.Product, bool, error) {
    /* an identifier no cache key can carry names no row, so it is answered as absent without asking the cache */
    if false == CacheSafeIdentifier(id) {
        return nil, false, nil
    }

    cacheKey := CacheKeyProductById(id)

    cached, rememberErr := rememberEntityOrAbsence(
        cache.For(runtimeInstance, instance.cache),
        cacheKey,
        func(flightRuntime melodycontainercontract.Runtime) (*entity.Product, error) {
            product, found, findErr := instance.productRepository.FindById(flightRuntime.Context(), id)
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

    return cached, true, nil
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

/* RecordView counts one read of a product on both counters and answers the counts so far. Each counter is the cache backend's atomic increment, shared by every process on the shared cache; they are hints rather than a ledger, since example:cache:clear and example:db:reset empty the namespace they live in. The window's counter is a fixed window: its expiry is written by the read that creates it and never moved by the later ones. */
func (instance *ProductService) RecordView(runtimeInstance melodycontainercontract.Runtime, id string) (ProductViews, error) {
    boundCache := cache.For(runtimeInstance, instance.cache)

    total, totalErr := boundCache.Increment(CacheKeyProductViews(id), 1, melodycachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != totalErr {
        return ProductViews{}, totalErr
    }

    daily, dailyErr := boundCache.Increment(CacheKeyProductDailyViews(id), 1, melodycachecontract.CounterOptions{Ttl: bound.Of(instance.productViewsWindow)})
    if nil != dailyErr {
        return ProductViews{}, dailyErr
    }

    return ProductViews{Total: total, Daily: daily}, nil
}

/* Views reads both counters without counting a read; a counter never written, or whose window closed, reads 0. */
func (instance *ProductService) Views(runtimeInstance melodycontainercontract.Runtime, id string) (ProductViews, error) {
    boundCache := cache.For(runtimeInstance, instance.cache)

    total, _, totalErr := boundCache.GetCounter(CacheKeyProductViews(id))
    if nil != totalErr {
        return ProductViews{}, totalErr
    }

    daily, _, dailyErr := boundCache.GetCounter(CacheKeyProductDailyViews(id))
    if nil != dailyErr {
        return ProductViews{}, dailyErr
    }

    return ProductViews{Total: total, Daily: daily}, nil
}

/* ViewsWindow is the span of the window counter, APP_PRODUCT_VIEWS_WINDOW */
func (instance *ProductService) ViewsWindow() time.Duration {
    return instance.productViewsWindow
}

func (instance *ProductService) Create(
    runtimeInstance melodycontainercontract.Runtime,
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

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventProductCreated, event.NewProductCreatedEvent(product), product.Id, CacheKeyProductList, CacheKeyProductById(product.Id))

    return product, nil
}

func (instance *ProductService) Update(
    runtimeInstance melodycontainercontract.Runtime,
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

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventProductUpdated, event.NewProductUpdatedEvent(&modified), modified.Id, CacheKeyProductList, CacheKeyProductById(modified.Id))

    return &modified, true, nil
}

func (instance *ProductService) DeleteById(
    runtimeInstance melodycontainercontract.Runtime,
    productId string,
) (bool, error) {
    deleted, deleteErr := instance.productRepository.DeleteById(WriteContext(runtimeInstance), productId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    dispatchCommitted(runtimeInstance, instance.eventDispatcher, instance.cache, event.EventProductDeleted, event.NewProductDeletedEvent(productId), productId, CacheKeyProductList, CacheKeyProductById(productId), CacheKeyProductViews(productId), CacheKeyProductDailyViews(productId))

    return true, nil
}

func MustGetProductService(ctx context.Context, resolver melodycontainercontract.Resolver) *ProductService {
    return container.MustFromResolver[*ProductService](
        ctx,
        resolver,
        ServiceProductService,
    )
}
