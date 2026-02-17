package bootstrap

import (
	"github.com/precision-soft/melody/v2/.example/domain/repository"
	"github.com/precision-soft/melody/v2/.example/domain/service"
	"github.com/precision-soft/melody/v2/.example/infra/cache"
	"github.com/precision-soft/melody/v2/.example/infra/repository"
	melodyapplication "github.com/precision-soft/melody/v2/application"
	melodycache "github.com/precision-soft/melody/v2/cache"
	melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
	melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
	melodyevent "github.com/precision-soft/melody/v2/event"
)

func registerServices(app *melodyapplication.Application) {
	app.RegisterService(
		melodycache.ServiceCacheSerializer,
		func(resolver melodycontainercontract.Resolver) (melodycachecontract.Serializer, error) {
			return cache.NewGobSerializer(), nil
		},
	)

	app.RegisterService(
		repository.ServiceCategoryRepository,
		inmemoryrepository.CategoryRepositoryProvider(),
	)

	app.RegisterService(
		repository.ServiceCurrencyRepository,
		inmemoryrepository.CurrencyRepositoryProvider(),
	)

	app.RegisterService(
		repository.ServiceProductRepository,
		inmemoryrepository.ProductRepositoryProvider(),
	)

	app.RegisterService(
		repository.ServiceUserRepository,
		inmemoryrepository.UserRepositoryProvider(),
	)

	app.RegisterService(
		service.ServiceCategoryService,
		func(resolver melodycontainercontract.Resolver) (*service.CategoryService, error) {
			categoryRepository := repository.MustGetCategoryRepository(resolver)
			cacheInstance := melodycache.CacheMustFromResolver(resolver)
			eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

			return service.NewCategoryService(categoryRepository, cacheInstance, eventDispatcher), nil
		},
	)

	app.RegisterService(
		service.ServiceCurrencyService,
		func(resolver melodycontainercontract.Resolver) (*service.CurrencyService, error) {
			currencyRepository := repository.MustGetCurrencyRepository(resolver)
			cacheInstance := melodycache.CacheMustFromResolver(resolver)
			eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

			return service.NewCurrencyService(currencyRepository, cacheInstance, eventDispatcher), nil
		},
	)

	app.RegisterService(
		service.ServiceUserService,
		func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
			userRepository := repository.MustUserRepository(resolver)
			cacheInstance := melodycache.CacheMustFromResolver(resolver)
			eventDispatcher := melodyevent.EventDispatcherMustFromResolver(resolver)

			return service.NewUserService(userRepository, cacheInstance, eventDispatcher), nil
		},
	)

	app.RegisterService(
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
