package config

import (
    "time"

    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/handler"
    "github.com/precision-soft/melody/v3/.example/handler/accesstoken"
    handlercategory "github.com/precision-soft/melody/v3/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/v3/.example/handler/currency"
    handlerevent "github.com/precision-soft/melody/v3/.example/handler/event"
    handleri18n "github.com/precision-soft/melody/v3/.example/handler/i18n"
    handlerinternalauth "github.com/precision-soft/melody/v3/.example/handler/internalauth"
    handleroutbox "github.com/precision-soft/melody/v3/.example/handler/outbox"
    handlerproduct "github.com/precision-soft/melody/v3/.example/handler/product"
    handlersecure "github.com/precision-soft/melody/v3/.example/handler/secure"
    handlerstorage "github.com/precision-soft/melody/v3/.example/handler/storage"
    handlertwofactor "github.com/precision-soft/melody/v3/.example/handler/twofactor"
    handleruser "github.com/precision-soft/melody/v3/.example/handler/user"
    "github.com/precision-soft/melody/v3/.example/route"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
)

func (instance *Module) RegisterHttpRoutes(kernelInstance melodykernelcontract.Kernel) {
    router := kernelInstance.HttpRouter()

    kernelInstance.HttpKernel().SetNotFoundHandler(handler.NotFoundHandler())

    /* @info the health and openapi routes opt into the frontend route manifest (melody:routes:manifest) as working proof of the export: exposed + zoned public, so the TypeScript RouteGenerator can build their URLs by name */
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

    router.HandleNamed("example.platform.check", "GET", "/platform/check", handler.PlatformCheckHandler())

    router.HandleNamed("example.messagebus.dispatch", "POST", "/messagebus/dispatch", handler.WelcomeEmailDispatchHandler())

    /* @info the example.metrics and example.websocket routes are contributed by the opentelemetry and websocket modules (see configure.go). */

    router.HandleNamed("example.encrypt.roundtrip", "GET", "/encrypt/roundtrip", handler.EncryptRoundTripHandler(instance.cipher))

    router.HandleNamed(route.AccessTokenIssueName, "POST", route.AccessTokenIssuePattern, accesstoken.IssueHandler())
    router.HandleNamed(route.AccessTokenRevokeDeviceName, "POST", route.AccessTokenRevokeDevicePattern, accesstoken.RevokeDeviceHandler())
    router.HandleNamed(route.AccessTokenRevokeUserName, "POST", route.AccessTokenRevokeUserPattern, accesstoken.RevokeUserHandler())
    router.HandleNamed(route.DeviceIdentityName, "GET", route.DeviceIdentityPattern, handlersecure.MeHandler())

    if nil != instance.redisClient {
        instance.buildCatalogWriteThrottle()
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

    /* @info the outbox handlers hold container.Lazy handles built at route-registration time: the store and
       relay services (provided by the outbox module's factories, see configure.go) are resolved at the first
       request, so registering the routes never touches the outbox schema or the transport. */
    if nil != instance.database {
        outboxStore := melodycontainer.Lazy[*outboxintegration.Store](kernelInstance.ServiceContainer(), outboxintegration.ServiceStore)
        outboxRelay := melodycontainer.Lazy[*outboxintegration.Relay](kernelInstance.ServiceContainer(), outboxintegration.ServiceRelay)

        router.HandleNamed("example.outbox.enqueue", "POST", "/outbox/enqueue", handleroutbox.EnqueueHandler(instance.database, outboxStore))
        router.HandleNamed("example.outbox.relay", "POST", "/outbox/relay", handleroutbox.RelayHandler(outboxRelay))
        router.HandleNamed("example.outbox.status", "GET", "/outbox/status", handleroutbox.StatusHandler(instance.database, outboxStore))
    }

    if nil != instance.storage {
        router.HandleNamed("example.storage.put", "POST", "/storage/object", handlerstorage.PutHandler(instance.storage))
        router.HandleNamed("example.storage.get", "GET", "/storage/object", handlerstorage.GetHandler(instance.storage))
    }

    router.HandleNamed(route.I18nGreetingName, "GET", route.I18nGreetingPattern, handleri18n.GreetingHandler())

    router.HandleNamed(route.EventsStreamName, "GET", route.EventsStreamPattern, handlerevent.StreamHandler(instance.serverSentEventHub))
    router.HandleNamed(route.EventsPublishName, "POST", route.EventsPublishPattern, handlerevent.PublishHandler(instance.messageBusDispatch))

    /* @info every catalog/user route below is exposed in the frontend zone: the admin SPA generates all of
       their URLs by name from the route manifest (data-route / route(...)), so an unexposed route would make
       the client throw "unknown route". */
    router.HandleWithOptions(route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler(), frontendRoute(route.CategoriesApiReadAllName, "GET"))

    router.HandleWithOptions(route.CurrenciesApiReadAllPattern, handlercurrency.ApiReadAllHandler(), frontendRoute(route.CurrenciesApiReadAllName, "GET"))

    router.HandleWithOptions(route.ProductsListPagePattern, handlerproduct.ListPageHandler(), frontendRoute(route.ProductsListPageName, "GET"))
    router.HandleWithOptions(route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler(), frontendRoute(route.ProductsCreatePageName, "GET"))
    router.HandleWithOptions(route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler(), frontendRoute(route.ProductsUpdatePageName, "GET"))
    router.HandleWithOptions(route.ProductsApiCreatePattern, instance.throttledWrite(handlerproduct.ApiCreateHandler()), frontendRoute(route.ProductsApiCreateName, "POST"))
    router.HandleWithOptions(route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler(), frontendRoute(route.ProductsApiReadAllName, "GET"))
    router.HandleWithOptions(route.ProductsApiReadPattern, handlerproduct.ApiReadHandler(), frontendRoute(route.ProductsApiReadName, "GET"))
    router.HandleWithOptions(route.ProductsApiUpdatePattern, instance.throttledWrite(handlerproduct.ApiUpdateHandler()), frontendRoute(route.ProductsApiUpdateName, "PUT"))
    router.HandleWithOptions(route.ProductsApiDeletePattern, instance.throttledWrite(handlerproduct.ApiDeleteHandler()), frontendRoute(route.ProductsApiDeleteName, "DELETE"))

    router.HandleWithOptions(route.UsersListPagePattern, handleruser.ListPageHandler(), frontendRoute(route.UsersListPageName, "GET"))
    router.HandleWithOptions(route.UsersCreatePagePattern, handleruser.CreatePageHandler(), frontendRoute(route.UsersCreatePageName, "GET"))
    router.HandleWithOptions(route.UsersUpdatePagePattern, handleruser.UpdatePageHandler(), frontendRoute(route.UsersUpdatePageName, "GET"))
    router.HandleWithOptions(route.UsersApiCreatePattern, instance.throttledWrite(handleruser.ApiCreateHandler()), frontendRoute(route.UsersApiCreateName, "POST"))
    router.HandleWithOptions(route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler(), frontendRoute(route.UsersApiReadAllName, "GET"))
    router.HandleWithOptions(route.UsersApiReadPattern, handleruser.ApiReadHandler(), frontendRoute(route.UsersApiReadName, "GET"))
    router.HandleWithOptions(route.UsersApiUpdatePattern, instance.throttledWrite(handleruser.ApiUpdateHandler()), frontendRoute(route.UsersApiUpdateName, "PUT"))
    router.HandleWithOptions(route.UsersApiDeletePattern, instance.throttledWrite(handleruser.ApiDeleteHandler()), frontendRoute(route.UsersApiDeleteName, "DELETE"))
}

/* frontendRoute marks a route as exposed in the frontend zone so its URL is generatable by name from the
   route manifest (window.melodyRoutes) that the admin SPA resolves data-route / route(...) calls against. */
func frontendRoute(name string, method string) melodyhttpcontract.RouteOptions {
    return melodyhttp.NewRouteOptions(name, []string{method}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZoneFrontend))
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)

/* buildCatalogWriteThrottle prepares the shared budget the nomenclature's write endpoints sit behind.

The counter lives in redis, so several replicas enforce one limit rather than each allowing its own, and the
client key is resolved trusted-proxy-aware: behind the compose load balancer the X-Forwarded-For client is
used, a direct hit falls back to the peer address, and a spoofed header from an untrusted peer is ignored.
With redis unreachable the limiter fails closed, so a write is refused rather than let through uncounted. */
func (instance *Module) buildCatalogWriteThrottle() {
    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(
        melodyrueidis.NewRateLimiter(
            instance.redisClient,
            catalogWriteAllowance,
            time.Minute,
            melodyrueidis.WithRateLimiterKeyPrefix(redisRateLimitKeyPrefix),
        ),
        nil,
        nil,
    )

    rateLimitConfig.SetClientIpResolver(melodyhttpmiddleware.NewForwardedClientIpResolver(melodyhttpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList:      []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
    }))

    instance.catalogWriteThrottle = melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)
}

/* throttledWrite puts an endpoint that changes the nomenclature behind the shared per-address budget. The
reads are left alone deliberately: a catalogue is meant to be browsed, and it is the writes that a runaway
script turns into damage.

Without redis there is no limiter and the handler is returned untouched, which is the same rule the rest of
the example follows — an integration the environment did not give it is absent rather than broken. */
func (instance *Module) throttledWrite(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
    if nil == instance.catalogWriteThrottle {
        return next
    }

    return instance.catalogWriteThrottle(next)
}
