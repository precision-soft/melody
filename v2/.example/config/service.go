package config

import (
    "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func (instance *Module) RegisterServices(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the live integrations are built before anything is registered, because what they yield decides which services and routes exist at all */
    instance.buildRedis(kernelInstance)
    instance.buildDatabase(kernelInstance)

    instance.registerRedisServices(registrar)
    instance.registerDatabaseServices(registrar)
    instance.registerCatalogJournalService(registrar)
    instance.registerCatalogReportService(registrar)

    registrar.RegisterService(
        melodycache.ServiceCacheSerializer,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
            return cache.NewGobSerializer(), nil
        },
    )

    /* the nomenclature is held wherever the environment can hold it: in the database when one was configured, and in memory otherwise. The repositories are told which by the name of the database service, and an empty name is how this module says there is none. */
    databaseServiceName := instance.databaseServiceName()

    registrar.RegisterService(
        repository.ServiceCategoryRepository,
        repository.CategoryRepositoryProvider(databaseServiceName),
    )

    registrar.RegisterService(
        repository.ServiceCurrencyRepository,
        repository.CurrencyRepositoryProvider(databaseServiceName),
    )

    registrar.RegisterService(
        repository.ServiceProductRepository,
        repository.ProductRepositoryProvider(databaseServiceName),
    )

    registrar.RegisterService(
        repository.ServiceUserRepository,
        repository.UserRepositoryProvider(databaseServiceName),
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
            clockInstance := melodyclock.ClockMustFromResolver(resolver)

            return service.NewProductService(
                productRepository,
                categoryService,
                currencyService,
                cacheInstance,
                eventDispatcher,
                clockInstance,
            ), nil
        },
    )
}

var _ melodyapplicationcontract.ServiceModule = (*Module)(nil)

/* registerCatalogJournalService wires the journal against whatever the environment gave the example. Without a database there is nowhere to keep a record of the changes, and the service is registered all the same with nothing behind it: the writes still succeed, and the report shows a journal of zero rather than the application refusing to change anything. */
func (instance *Module) registerCatalogJournalService(registrar melodyapplicationcontract.ServiceRegistrar) {
    hasDatabase := nil != instance.databaseRegistry

    registrar.RegisterService(
        service.ServiceCatalogJournalService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogJournalService, error) {
            clockInstance := melodyclock.ClockMustFromResolver(resolver)

            if false == hasDatabase {
                return service.NewCatalogJournalService(nil, clockInstance), nil
            }

            return service.NewCatalogJournalService(
                repository.MustGetCatalogJournalRepository(resolver),
                clockInstance,
            ), nil
        },
    )
}

/* registerCatalogReportService wires the report against whatever the environment gave the example: the redis backend when one was built, and the journal when a database was configured. Both are optional, and the service degrades to what it can actually reach rather than being absent. */
func (instance *Module) registerCatalogReportService(registrar melodyapplicationcontract.ServiceRegistrar) {
    cacheBackend := instance.redisCacheBackend
    hasDatabase := nil != instance.databaseRegistry

    registrar.RegisterService(
        service.ServiceCatalogReportService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogReportService, error) {
            clockInstance := melodyclock.ClockMustFromResolver(resolver)
            productRepository := repository.MustGetProductRepository(resolver)

            var backend service.CatalogReportBackend
            if nil != cacheBackend {
                backend = cacheBackend
            }

            if false == hasDatabase {
                return service.NewCatalogReportService(clockInstance, backend, productRepository, nil), nil
            }

            return service.NewCatalogReportService(
                clockInstance,
                backend,
                productRepository,
                repository.MustGetCatalogJournalRepository(resolver),
            ), nil
        },
    )
}
