package config

import (
    "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyevent "github.com/precision-soft/melody/event"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func (instance *Module) RegisterServices(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        melodycache.ServiceCacheSerializer,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
            return cache.NewGobSerializer(), nil
        },
    )

    registrar.RegisterService(
        repository.ServiceCategoryRepository,
        repository.CategoryRepositoryProvider(),
    )

    registrar.RegisterService(
        repository.ServiceCurrencyRepository,
        repository.CurrencyRepositoryProvider(),
    )

    registrar.RegisterService(
        repository.ServiceProductRepository,
        repository.ProductRepositoryProvider(),
    )

    registrar.RegisterService(
        repository.ServiceUserRepository,
        repository.UserRepositoryProvider(),
    )

    registrar.RegisterService(
        service.ServiceCategoryService,
        func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
            categoryRepository := repository.MustGetCategoryRepository(resolver)
            cacheInstance := melodycache.CacheMustFromResolver(resolver)
            eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

            return service.NewCategoryService(categoryRepository, cacheInstance, eventDispatcher), nil
        },
    )

    registrar.RegisterService(
        service.ServiceCurrencyService,
        func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
            currencyRepository := repository.MustGetCurrencyRepository(resolver)
            cacheInstance := melodycache.CacheMustFromResolver(resolver)
            eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

            return service.NewCurrencyService(currencyRepository, cacheInstance, eventDispatcher), nil
        },
    )

    registrar.RegisterService(
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            userRepository := repository.MustGetUserRepository(resolver)
            cacheInstance := melodycache.CacheMustFromResolver(resolver)
            eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

            return service.NewUserService(userRepository, cacheInstance, eventDispatcher), nil
        },
    )

    registrar.RegisterService(
        service.ServiceProductService,
        func(resolver melodycontainercontract.Resolver) (*service.ProductService, error) {
            productRepository := repository.MustGetProductRepository(resolver)
            categoryService := service.MustGetCategoryService(resolver)
            currencyService := service.MustGetCurrencyService(resolver)
            cacheInstance := melodycache.CacheMustFromResolver(resolver)
            eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

            return service.NewProductService(
                productRepository,
                categoryService,
                currencyService,
                cacheInstance,
                eventDispatcher,
            ), nil
        },
    )
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)
