package config

import (
    "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyevent "github.com/precision-soft/melody/event"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func (instance *Module) RegisterServices(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the live integrations are built before anything is registered, because what they yield decides which services and routes exist at all */
    instance.buildRedis(kernelInstance)
    instance.buildDatabase(kernelInstance)

    instance.registerRedisServices(registrar)
    instance.registerDatabaseServices(registrar)
    instance.registerCatalogReportService(registrar)

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

/* registerCatalogReportService wires the report against whatever the environment gave the example: the redis backend when one was built, and the note repository when a database was configured. Both are optional, and the service degrades to what it can actually reach rather than being absent. */
func (instance *Module) registerCatalogReportService(registrar melodyapplicationcontract.ServiceRegistrar) {
    cacheBackend := instance.redisCacheBackend
    hasDatabase := nil != instance.databaseRegistry

    registrar.RegisterService(
        service.ServiceCatalogReportService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogReportService, error) {
            clockInstance := melodyclock.ClockMustFromResolver(resolver)

            var backend service.CatalogReportBackend
            if nil != cacheBackend {
                backend = cacheBackend
            }

            if false == hasDatabase {
                return service.NewCatalogReportService(clockInstance, backend, nil), nil
            }

            return service.NewCatalogReportService(
                clockInstance,
                backend,
                repository.MustGetCatalogNoteRepository(resolver),
            ), nil
        },
    )
}
