package config

import (
    "path/filepath"

    "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyevent "github.com/precision-soft/melody/event"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
    melodysession "github.com/precision-soft/melody/session"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
)

func (instance *Module) RegisterServices(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the api token is captured here because RegisterSecurity receives only the builder: this hook runs first and holds the kernel, so the firewall declaration reads the value from the module */
    instance.apiToken = parameterValue(kernelInstance, ParameterApiToken)

    /* the live integrations are built before anything is registered, because what they yield decides which services and routes exist at all */
    instance.buildRedis(kernelInstance)
    instance.buildDatabase(kernelInstance)

    instance.registerSessionStorage(kernelInstance, registrar)

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

/* registerSessionStorage swaps the framework's in-memory default for the file-backed storage when the environment names a file, so a signed-in session survives a process restart. The registration wins because module services land before the framework's own has-guarded fallback; an empty value keeps the default, like every other switch of the example. */
func (instance *Module) registerSessionStorage(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.ServiceRegistrar) {
    sessionFilePath := resolvedSessionFilePath(
        parameterValue(kernelInstance, ParameterSessionFile),
        kernelInstance.Config().Kernel().ProjectDir(),
    )
    if "" == sessionFilePath {
        return
    }

    registrar.RegisterService(
        melodysession.ServiceSessionStorage,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
            return melodysession.NewFileStorageFromPath(sessionFilePath)
        },
    )
}

/* resolvedSessionFilePath keeps the empty value empty — the switch that says "in-memory" — and anchors a relative path to the project directory rather than the working directory, so a console run and the http process read the same snapshot. */
func resolvedSessionFilePath(sessionFilePath string, projectDirectory string) string {
    if "" == sessionFilePath {
        return ""
    }

    if false == filepath.IsAbs(sessionFilePath) {
        return filepath.Join(projectDirectory, sessionFilePath)
    }

    return sessionFilePath
}

/* registerCatalogJournalService wires the journal against whatever the environment gave the example. Without a journal database there is nowhere to keep a record of the changes, and the service is registered all the same with nothing behind it: the writes still succeed, and the report shows a journal of zero rather than the application refusing to change anything. */
func (instance *Module) registerCatalogJournalService(registrar melodyapplicationcontract.ServiceRegistrar) {
    hasJournal := instance.databaseWiring.journal

    registrar.RegisterService(
        service.ServiceCatalogJournalService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogJournalService, error) {
            clockInstance := melodyclock.ClockMustFromResolver(resolver)

            if false == hasJournal {
                return service.NewCatalogJournalService(nil, clockInstance), nil
            }

            /* the handle is built over this provider's resolver, which records the teardown edge; nothing dials the journal database until the first recorded change resolves through it */
            return service.NewCatalogJournalService(
                melodycontainer.Lazy[repository.CatalogJournalRepository](resolver, repository.ServiceCatalogJournalRepository),
                clockInstance,
            ), nil
        },
    )
}

/* registerCatalogReportService wires the report against whatever the environment gave the example: the redis backend when one was built, and the journal when its database was configured. Both are optional, and the service degrades to what it can actually reach rather than being absent. */
func (instance *Module) registerCatalogReportService(registrar melodyapplicationcontract.ServiceRegistrar) {
    cacheBackend := instance.redisCacheBackend
    hasJournal := instance.databaseWiring.journal

    registrar.RegisterService(
        service.ServiceCatalogReportService,
        func(resolver melodycontainercontract.Resolver) (*service.CatalogReportService, error) {
            clockInstance := melodyclock.ClockMustFromResolver(resolver)
            productRepository := repository.MustGetProductRepository(resolver)

            var backend service.CatalogReportBackend
            if nil != cacheBackend {
                backend = cacheBackend
            }

            if false == hasJournal {
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
