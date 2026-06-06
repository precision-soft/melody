package config

import (
    "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodymailer "github.com/precision-soft/melody/v3/mailer"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
    melodytranslation "github.com/precision-soft/melody/v3/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

func (instance *Module) RegisterServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        melodycache.ServiceCacheSerializer,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
            return cache.NewGobSerializer(), nil
        },
    )

    registrar.RegisterService(
        melodymessagebus.ServiceBus,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Bus, error) {
            return instance.messageBusDispatch, nil
        },
    )

    registrar.RegisterService(
        melodytranslation.ServiceTranslator,
        func(resolver melodycontainercontract.Resolver) (melodytranslationcontract.Translator, error) {
            return instance.translator, nil
        },
    )

    registrar.RegisterService(
        melodymailer.ServiceMailer,
        func(resolver melodycontainercontract.Resolver) (melodymailercontract.Mailer, error) {
            return instance.mailer, nil
        },
    )

    registrar.RegisterService(
        melodyopenapi.ServiceOpenApiRegistry,
        func(resolver melodycontainercontract.Resolver) (*melodyopenapi.Registry, error) {
            return instance.openApiRegistry, nil
        },
    )

    instance.registerStorageService(registrar)
    instance.registerLockerService(registrar)
    instance.registerRedisServices(registrar)

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
