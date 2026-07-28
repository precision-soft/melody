package service

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/event"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodyclockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    ServiceProductService = "service-example-product-service"
)

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

/* ProductService stamps every write with the injected clock rather than the wall, which is what makes the stamp assertable: a frozen clock lets a test state the exact instant a product carries, which cannot be written against time.Now. */
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
    cacheKey := CacheKeyProductById(id)

    cached, rememberErr := cache.Remember(
        instance.cache,
        cacheKey,
        0,
        func(ctx context.Context) (any, error) {
            product, found, findErr := instance.productRepository.FindById(ctx, id)
            if nil != findErr {
                return nil, findErr
            }

            if false == found {
                return nil, nil
            }

            return product, nil
        },
        nil,
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

    createErr := instance.productRepository.Create(runtimeInstance.Context(), product)
    if nil != createErr {
        return nil, createErr
    }

    createdEvent := event.NewProductCreatedEvent(product)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.ProductCreatedEventName,
        createdEvent,
    )
    if nil != dispatchErr {
        return nil, dispatchErr
    }

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
    ctx := runtimeInstance.Context()

    product, found, findErr := instance.productRepository.FindById(ctx, id)
    if nil != findErr {
        return nil, false, findErr
    }

    if false == found {
        return nil, false, nil
    }

    product.Name = name
    product.Description = description
    product.CategoryId = categoryId
    product.Price = price
    product.CurrencyId = currencyId
    product.Stock = stock
    product.UpdatedAt = instance.clock.Now()

    updated, updateErr := instance.productRepository.Update(ctx, product)
    if nil != updateErr {
        return nil, false, updateErr
    }
    if false == updated {
        return nil, false, nil
    }

    productUpdatedEvent := event.NewProductUpdatedEvent(product)

    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.ProductUpdatedEventName,
        productUpdatedEvent,
    )
    if nil != dispatchErr {
        return nil, true, dispatchErr
    }

    return product, true, nil
}

func (instance *ProductService) DeleteById(
    runtimeInstance melodyruntimecontract.Runtime,
    productId string,
) (bool, error) {
    deleted, deleteErr := instance.productRepository.DeleteById(runtimeInstance.Context(), productId)
    if nil != deleteErr {
        return false, deleteErr
    }
    if false == deleted {
        return false, nil
    }

    deletedEvent := event.NewProductDeletedEvent(productId)
    _, dispatchErr := instance.eventDispatcher.DispatchName(
        runtimeInstance,
        event.ProductDeletedEventName,
        deletedEvent,
    )
    if nil != dispatchErr {
        return true, dispatchErr
    }

    return true, nil
}

func MustGetProductService(resolver melodycontainercontract.Resolver) *ProductService {
    return container.MustFromResolver[*ProductService](
        resolver,
        ServiceProductService,
    )
}
