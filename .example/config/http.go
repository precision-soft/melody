package config

import (
    "github.com/precision-soft/melody/.example/handler"
    handlercategory "github.com/precision-soft/melody/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/.example/handler/currency"
    handlerproduct "github.com/precision-soft/melody/.example/handler/product"
    handleruser "github.com/precision-soft/melody/.example/handler/user"
    "github.com/precision-soft/melody/.example/route"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func (instance *Module) RegisterHttpRoutes(kernelInstance melodykernelcontract.Kernel) {
    router := kernelInstance.HttpRouter()

    kernelInstance.HttpKernel().SetNotFoundHandler(handler.NotFoundHandler())

    router.HandleNamed("example.health", "GET", "/health", handler.HealthHandler())

    router.HandleNamed(route.LoginPageName, "GET", route.LoginPagePattern, handler.LoginPageHandler())
    router.HandleNamed(route.LoginSubmitName, "POST", route.LoginSubmitPattern, handler.LoginHandler())
    router.HandleNamed(route.LogoutName, "GET", route.LogoutPattern, handler.LogoutHandler())

    router.HandleNamed(route.RoutesName, "GET", route.RoutesPattern, handler.RoutesHandler())

    instance.registerIntegrationRoutes(router)

    router.HandleNamed(route.CategoriesApiReadAllName, "GET", route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler())

    router.HandleNamed(route.CurrenciesApiReadAllName, "GET", route.CurrenciesApiReadAllPattern, handlercurrency.ApiReadAllHandler())

    router.HandleNamed(route.ProductsListPageName, "GET", route.ProductsListPagePattern, handlerproduct.ListPageHandler())
    router.HandleNamed(route.ProductsCreatePageName, "GET", route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler())
    router.HandleNamed(route.ProductsUpdatePageName, "GET", route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler())
    router.HandleNamed(route.ProductsApiCreateName, "POST", route.ProductsApiCreatePattern, handlerproduct.ApiCreateHandler())
    router.HandleNamed(route.ProductsApiReadAllName, "GET", route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler())
    router.HandleNamed(route.ProductsApiReadName, "GET", route.ProductsApiReadPattern, handlerproduct.ApiReadHandler())
    router.HandleNamed(route.ProductsApiUpdateName, "PUT", route.ProductsApiUpdatePattern, handlerproduct.ApiUpdateHandler())
    router.HandleNamed(route.ProductsApiDeleteName, "DELETE", route.ProductsApiDeletePattern, handlerproduct.ApiDeleteHandler())

    router.HandleNamed(route.UsersListPageName, "GET", route.UsersListPagePattern, handleruser.ListPageHandler())
    router.HandleNamed(route.UsersCreatePageName, "GET", route.UsersCreatePagePattern, handleruser.CreatePageHandler())
    router.HandleNamed(route.UsersUpdatePageName, "GET", route.UsersUpdatePagePattern, handleruser.UpdatePageHandler())
    router.HandleNamed(route.UsersApiCreateName, "POST", route.UsersApiCreatePattern, handleruser.ApiCreateHandler())
    router.HandleNamed(route.UsersApiReadAllName, "GET", route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler())
    router.HandleNamed(route.UsersApiReadName, "GET", route.UsersApiReadPattern, handleruser.ApiReadHandler())
    router.HandleNamed(route.UsersApiUpdateName, "PUT", route.UsersApiUpdatePattern, handleruser.ApiUpdateHandler())
    router.HandleNamed(route.UsersApiDeleteName, "DELETE", route.UsersApiDeletePattern, handleruser.ApiDeleteHandler())
}

/* registerIntegrationRoutes declares only what the environment actually gave the example. A demo route whose backend was never wired would answer 500 to every request and say nothing about why, so an unconfigured integration simply has no route. */
func (instance *Module) registerIntegrationRoutes(router melodyhttpcontract.Router) {
    if nil != instance.redisCacheBackend {
        router.HandleNamed(
            route.IntegrationCacheName,
            "GET",
            route.IntegrationCachePattern,
            handler.CacheDemoHandler(instance.redisCacheBackend),
        )
    }

    if nil != instance.redisRateLimiter {
        rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(instance.redisRateLimiter, nil, nil)

        router.HandleNamed(
            route.IntegrationRateLimitName,
            "GET",
            route.IntegrationRateLimitPattern,
            melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)(handler.RateLimitDemoHandler()),
        )
    }

    if nil != instance.databaseRegistry {
        router.HandleNamed(
            route.IntegrationDatabaseName,
            "GET",
            route.IntegrationDatabasePattern,
            handler.DatabaseDemoHandler(),
        )
    }

    router.HandleNamed(
        route.IntegrationReportName,
        "GET",
        route.IntegrationReportPattern,
        handler.ReportDemoHandler(),
    )
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)
