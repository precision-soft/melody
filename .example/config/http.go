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
    kernelInstance.HttpKernel().SetForwardedHeadersPolicy(exampleForwardedHeadersPolicy())

    router.HandleNamed("example.health", "GET", "/health", handler.HealthHandler())

    router.HandleNamed(route.LoginPageName, "GET", route.LoginPagePattern, handler.LoginPageHandler())
    router.HandleNamed(route.LoginSubmitName, "POST", route.LoginSubmitPattern, instance.throttledWrite(handler.LoginHandler()))
    router.HandleNamed(route.LogoutName, "GET", route.LogoutPattern, handler.LogoutHandler())

    router.HandleNamed(route.RoutesName, "GET", route.RoutesPattern, handler.RoutesHandler())

    instance.registerCatalogReportRoute(router)

    router.HandleNamed(route.CategoriesApiReadAllName, "GET", route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler())

    router.HandleNamed(route.CurrenciesApiReadAllName, "GET", route.CurrenciesApiReadAllPattern, handlercurrency.ApiReadAllHandler())

    router.HandleNamed(route.ProductsListPageName, "GET", route.ProductsListPagePattern, handlerproduct.ListPageHandler())
    router.HandleNamed(route.ProductsCreatePageName, "GET", route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler())
    router.HandleNamed(route.ProductsUpdatePageName, "GET", route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler())
    router.HandleNamed(route.ProductsApiCreateName, "POST", route.ProductsApiCreatePattern, instance.throttledWrite(handlerproduct.ApiCreateHandler()))
    router.HandleNamed(route.ProductsApiReadAllName, "GET", route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler())
    router.HandleNamed(route.ProductsApiReadName, "GET", route.ProductsApiReadPattern, handlerproduct.ApiReadHandler())
    router.HandleNamed(route.ProductsApiUpdateName, "PUT", route.ProductsApiUpdatePattern, instance.throttledWrite(handlerproduct.ApiUpdateHandler()))
    router.HandleNamed(route.ProductsApiDeleteName, "DELETE", route.ProductsApiDeletePattern, instance.throttledWrite(handlerproduct.ApiDeleteHandler()))

    router.HandleNamed(route.UsersListPageName, "GET", route.UsersListPagePattern, handleruser.ListPageHandler())
    router.HandleNamed(route.UsersCreatePageName, "GET", route.UsersCreatePagePattern, handleruser.CreatePageHandler())
    router.HandleNamed(route.UsersUpdatePageName, "GET", route.UsersUpdatePagePattern, handleruser.UpdatePageHandler())
    router.HandleNamed(route.UsersApiCreateName, "POST", route.UsersApiCreatePattern, instance.throttledWrite(handleruser.ApiCreateHandler()))
    router.HandleNamed(route.UsersApiReadAllName, "GET", route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler())
    router.HandleNamed(route.UsersApiReadName, "GET", route.UsersApiReadPattern, handleruser.ApiReadHandler())
    router.HandleNamed(route.UsersApiUpdateName, "PUT", route.UsersApiUpdatePattern, instance.throttledWrite(handleruser.ApiUpdateHandler()))
    router.HandleNamed(route.UsersApiDeleteName, "DELETE", route.UsersApiDeletePattern, instance.throttledWrite(handleruser.ApiDeleteHandler()))
}

/* throttledWrite puts an endpoint behind the shared per-address budget. The catalogue writes go behind it because it is the writes that a runaway script turns into damage; the login submit goes behind it too, because an unthrottled credential door is a password-guessing and username-timing surface a browsed read is not. The reads are left alone deliberately: a catalogue is meant to be browsed.

   Without redis there is no limiter and the handler is returned untouched, which is the same rule the rest of the example follows — an integration the environment did not give it is absent rather than broken. */
func (instance *Module) throttledWrite(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
    if nil == instance.redisRateLimiter {
        return next
    }

    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(instance.redisRateLimiter, nil, nil)
    rateLimitConfig.SetClientIpResolver(melodyhttpmiddleware.NewForwardedClientIpResolver(exampleForwardedHeadersPolicy()))

    return melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)(next)
}

/* exampleForwardedHeadersPolicy is the ONE trust list of the example, read by the kernel for the scheme and by the limiter for the client address, so the two cannot disagree about which peer is an edge. Loopback is on it because the development stack and the live harness reach the application directly; a forwarded proto from those peers becomes believable too, which is the price of one list and is fine for a showcase that never terminates tls. */
func exampleForwardedHeadersPolicy() melodyhttpcontract.ForwardedHeadersPolicy {
    return melodyhttpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList: []string{
            "127.0.0.0/8",
            "::1/128",
            "10.0.0.0/8",
            "172.16.0.0/12",
            "192.168.0.0/16",
        },
    }
}

/* registerCatalogReportRoute declares the one reading the nomenclature publishes. It needs no backend of its own: with a cache it is served from the reading the scheduled refresh left there, and without one it is computed on the spot. */
func (instance *Module) registerCatalogReportRoute(router melodyhttpcontract.Router) {
    router.HandleNamed(
        route.CatalogReportName,
        "GET",
        route.CatalogReportPattern,
        handler.CatalogReportHandler(),
    )
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)
