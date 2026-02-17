package service

import (
	"context"
	"fmt"
	"time"

	"github.com/precision-soft/melody/v2/.example/domain/entity"
	"github.com/precision-soft/melody/v2/.example/domain/event"
	"github.com/precision-soft/melody/v2/.example/domain/repository"
	"github.com/precision-soft/melody/v2/cache"
	melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
	"github.com/precision-soft/melody/v2/container"
	melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
	melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
) *ProductService {
	return &ProductService{
		productRepository: productRepository,
		categoryService:   categoryService,
		currencyService:   currencyService,
		cache:             cacheInstance,
		eventDispatcher:   eventDispatcher,
	}
}

type ProductService struct {
	productRepository repository.ProductRepository
	categoryService   *CategoryService
	currencyService   *CurrencyService
	cache             melodycachecontract.Cache
	eventDispatcher   melodyeventcontract.EventDispatcher
}

func (instance *ProductService) List() ([]*entity.Product, error) {
	products, rememberErr := cache.Remember(
		instance.cache,
		CacheKeyProductList,
		0,
		func(ctx context.Context) (any, error) {
			return instance.productRepository.All(), nil
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
			product, found := instance.productRepository.FindById(id)
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
	now := time.Now()
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

	createErr := instance.productRepository.Create(product)
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
	product, found := instance.productRepository.FindById(id)
	if false == found {
		return nil, false, nil
	}

	product.Name = name
	product.Description = description
	product.CategoryId = categoryId
	product.Price = price
	product.CurrencyId = currencyId
	product.Stock = stock
	product.UpdatedAt = time.Now()

	updated, updateErr := instance.productRepository.Update(product)
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
	deleted, deleteErr := instance.productRepository.DeleteById(productId)
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
