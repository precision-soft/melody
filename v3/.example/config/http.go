package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/handler"
    handlercategory "github.com/precision-soft/melody/v3/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/v3/.example/handler/currency"
    handlerevents "github.com/precision-soft/melody/v3/.example/handler/events"
    handleri18n "github.com/precision-soft/melody/v3/.example/handler/i18n"
    handlerinternalauth "github.com/precision-soft/melody/v3/.example/handler/internalauth"
    handleroutbox "github.com/precision-soft/melody/v3/.example/handler/outbox"
    handlerproduct "github.com/precision-soft/melody/v3/.example/handler/product"
    handlersecure "github.com/precision-soft/melody/v3/.example/handler/secure"
    handlerstorage "github.com/precision-soft/melody/v3/.example/handler/storage"
    handlertwofactor "github.com/precision-soft/melody/v3/.example/handler/twofactor"
    handleruser "github.com/precision-soft/melody/v3/.example/handler/user"
    handlerwebsocketdemo "github.com/precision-soft/melody/v3/.example/handler/websocketdemo"
    "github.com/precision-soft/melody/v3/.example/route"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
)

func (instance *Module) RegisterHttpRoutes(kernelInstance melodykernelcontract.Kernel) {
    router := kernelInstance.HttpRouter()

    kernelInstance.HttpKernel().SetNotFoundHandler(handler.NotFoundHandler())

    /* @info the health and openapi routes opt into the frontend route manifest (melody:routes:manifest) as a working demo of the export: exposed + zoned public, so the TypeScript RouteGenerator can build their URLs by name */
    router.HandleWithOptions(
        "/health",
        handler.HealthHandler(),
        melodyhttp.NewRouteOptions("example.health", []string{"GET"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )

    router.HandleWithOptions(
        "/openapi.json",
        melodyopenapi.SpecHandler(instance.openApiInfo, instance.openApiRegistry),
        melodyhttp.NewRouteOptions("example.openapi", []string{"GET"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )

    router.HandleNamed("example.platform.demo", "GET", "/platform/demo", handler.PlatformDemoHandler())

    router.HandleNamed("example.messagebus.demo", "POST", "/messagebus/demo", handler.MessageBusDemoHandler())

    /* @info the example.metrics and example.websocket routes are contributed by the opentelemetry and websocket modules (see configure.go). */

    router.HandleNamed("example.cache.demo", "GET", "/cache/demo", handler.CacheDemoHandler())

    /* @info exposed to the route manifest so the admin nav can link to it by name (data-route="example.websocket.demo"). */
    router.HandleWithOptions(
        "/websocket/demo",
        handlerwebsocketdemo.PageHandler(),
        melodyhttp.NewRouteOptions("example.websocket.demo", []string{"GET"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )

    router.HandleNamed("example.encrypt.demo", "GET", "/encrypt/demo", handler.EncryptDemoHandler(instance.cipher))

    if nil != instance.redisClient {
        router.HandleNamed("example.redis.token.demo", "GET", "/redis/token/demo", handler.RedisTokenDemoHandler())

        /* @info distributed rate-limit demo: the counter lives in redis, so N replicas enforce ONE
           shared limit (the in-process limiters would each allow their own budget). The client key is
           resolved trusted-proxy-aware — behind the compose load balancer the X-Forwarded-For client is
           used, direct hits fall back to the peer address. Fail-closed: with redis down the route denies. */
        rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(
            melodyrueidis.NewRateLimiter(instance.redisClient, 5, time.Minute, melodyrueidis.WithRateLimiterKeyPrefix("melody-example:rate_limit:")),
            nil,
            nil,
        )
        rateLimitConfig.SetClientIpResolver(melodyhttpmiddleware.NewForwardedClientIpResolver(melodyhttpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: true,
            TrustedProxyList:      []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
        }))

        router.HandleNamed(
            "example.ratelimit.demo",
            "GET",
            "/ratelimit/demo",
            melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)(handler.HealthHandler()),
        )
    }

    if nil != instance.database {
        router.HandleNamed("example.database.demo", "GET", "/database/demo", handler.DatabaseDemoHandler(instance.database))
        router.HandleNamed("example.database.audit.demo", "GET", "/database/audit/demo", handler.AuditDemoHandler(instance.database))
    }

    router.HandleNamed(route.LoginPageName, "GET", route.LoginPagePattern, handler.LoginPageHandler())

    /* @info login-submit and logout are exposed to the route manifest (window.melodyRoutes) because the
       frontend resolves their URLs by name — the login form posts to route("example.login.submit") and the
       nav logs out via route("example.logout"); an unexposed route would make the client throw "unknown route". */
    router.HandleWithOptions(
        route.LoginSubmitPattern,
        handler.LoginHandler(),
        melodyhttp.NewRouteOptions(route.LoginSubmitName, []string{"POST"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )
    router.HandleWithOptions(
        route.LogoutPattern,
        handler.LogoutHandler(),
        melodyhttp.NewRouteOptions(route.LogoutName, []string{"GET"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )

    router.HandleWithOptions(route.RoutesPattern, handler.RoutesHandler(), frontendRoute(route.RoutesName, "GET"))

    router.HandleNamed(route.SecureMeName, "GET", route.SecureMePattern, handlersecure.MeHandler())

    router.HandleNamed(route.InternalWhoamiName, "POST", route.InternalWhoamiPattern, handlerinternalauth.WhoamiHandler())

    if nil != instance.twoFactorStore {
        router.HandleNamed("example.twofactor.enroll", "POST", "/twofactor/enroll", handlertwofactor.EnrollHandler(instance.twoFactorStore))
        router.HandleNamed("example.twofactor.verify", "POST", "/twofactor/verify", handlertwofactor.VerifyHandler(instance.twoFactorStore))
    }

    if nil != instance.outboxRelay {
        router.HandleNamed("example.outbox.enqueue", "POST", "/outbox/enqueue", handleroutbox.EnqueueHandler(instance.database, instance.outboxStore))
        router.HandleNamed("example.outbox.relay", "POST", "/outbox/relay", handleroutbox.RelayHandler(instance.outboxRelay))
        router.HandleNamed("example.outbox.status", "GET", "/outbox/status", handleroutbox.StatusHandler(instance.database))
    }

    if nil != instance.storage {
        router.HandleNamed("example.storage.put", "POST", "/storage/demo", handlerstorage.PutHandler(instance.storage))
        router.HandleNamed("example.storage.get", "GET", "/storage/demo", handlerstorage.GetHandler(instance.storage))
    }

    router.HandleNamed(route.I18nGreetingName, "GET", route.I18nGreetingPattern, handleri18n.GreetingHandler())

    router.HandleNamed(route.EventsStreamName, "GET", route.EventsStreamPattern, handlerevents.StreamHandler(instance.serverSentEventHub))
    router.HandleNamed(route.EventsPublishName, "GET", route.EventsPublishPattern, handlerevents.PublishHandler(instance.messageBusDispatch))

    /* @info every catalog/user route below is exposed in the frontend zone: the admin SPA generates all of
       their URLs by name from the route manifest (data-route / route(...)), so an unexposed route would make
       the client throw "unknown route". */
    router.HandleWithOptions(route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler(), frontendRoute(route.CategoriesApiReadAllName, "GET"))

    router.HandleWithOptions(route.CurrenciesApiReadAllPattern, handlercurrency.ApiReadAllHandler(), frontendRoute(route.CurrenciesApiReadAllName, "GET"))

    router.HandleWithOptions(route.ProductsListPagePattern, handlerproduct.ListPageHandler(), frontendRoute(route.ProductsListPageName, "GET"))
    router.HandleWithOptions(route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler(), frontendRoute(route.ProductsCreatePageName, "GET"))
    router.HandleWithOptions(route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler(), frontendRoute(route.ProductsUpdatePageName, "GET"))
    router.HandleWithOptions(route.ProductsApiCreatePattern, handlerproduct.ApiCreateHandler(), frontendRoute(route.ProductsApiCreateName, "POST"))
    router.HandleWithOptions(route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler(), frontendRoute(route.ProductsApiReadAllName, "GET"))
    router.HandleWithOptions(route.ProductsApiReadPattern, handlerproduct.ApiReadHandler(), frontendRoute(route.ProductsApiReadName, "GET"))
    router.HandleWithOptions(route.ProductsApiUpdatePattern, handlerproduct.ApiUpdateHandler(), frontendRoute(route.ProductsApiUpdateName, "PUT"))
    router.HandleWithOptions(route.ProductsApiDeletePattern, handlerproduct.ApiDeleteHandler(), frontendRoute(route.ProductsApiDeleteName, "DELETE"))

    router.HandleWithOptions(route.UsersListPagePattern, handleruser.ListPageHandler(), frontendRoute(route.UsersListPageName, "GET"))
    router.HandleWithOptions(route.UsersCreatePagePattern, handleruser.CreatePageHandler(), frontendRoute(route.UsersCreatePageName, "GET"))
    router.HandleWithOptions(route.UsersUpdatePagePattern, handleruser.UpdatePageHandler(), frontendRoute(route.UsersUpdatePageName, "GET"))
    router.HandleWithOptions(route.UsersApiCreatePattern, handleruser.ApiCreateHandler(), frontendRoute(route.UsersApiCreateName, "POST"))
    router.HandleWithOptions(route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler(), frontendRoute(route.UsersApiReadAllName, "GET"))
    router.HandleWithOptions(route.UsersApiReadPattern, handleruser.ApiReadHandler(), frontendRoute(route.UsersApiReadName, "GET"))
    router.HandleWithOptions(route.UsersApiUpdatePattern, handleruser.ApiUpdateHandler(), frontendRoute(route.UsersApiUpdateName, "PUT"))
    router.HandleWithOptions(route.UsersApiDeletePattern, handleruser.ApiDeleteHandler(), frontendRoute(route.UsersApiDeleteName, "DELETE"))
}

/* frontendRoute marks a route as exposed in the frontend zone so its URL is generatable by name from the
   route manifest (window.melodyRoutes) that the admin SPA resolves data-route / route(...) calls against. */
func frontendRoute(name string, method string) melodyhttpcontract.RouteOptions {
    return melodyhttp.NewRouteOptions(name, []string{method}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZoneFrontend))
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)
