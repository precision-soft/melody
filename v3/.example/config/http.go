package config

import (
    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/handler"
    "github.com/precision-soft/melody/v3/.example/handler/accesstoken"
    handlercategory "github.com/precision-soft/melody/v3/.example/handler/category"
    handlerreport "github.com/precision-soft/melody/v3/.example/handler/report"
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
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
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
    kernelInstance.HttpKernel().SetForwardedHeadersPolicy(exampleForwardedHeadersPolicy())

    /* the health and openapi routes opt into the frontend route manifest (melody:routes:manifest) as working proof of the export: exposed + zoned public, so the TypeScript RouteGenerator can build their URLs by name */
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

    /* the example.metrics and example.websocket routes are contributed by the opentelemetry and websocket modules (see configure.go). */

    router.HandleNamed("example.encrypt.roundtrip", "GET", "/encrypt/roundtrip", handler.EncryptRoundTripHandler(instance.cipher))

    router.HandleNamed(route.AccessTokenIssueName, "POST", route.AccessTokenIssuePattern, accesstoken.IssueHandler())
    router.HandleNamed(route.AccessTokenRevokeDeviceName, "POST", route.AccessTokenRevokeDevicePattern, accesstoken.RevokeDeviceHandler())
    router.HandleNamed(route.AccessTokenRevokeUserName, "POST", route.AccessTokenRevokeUserPattern, accesstoken.RevokeUserHandler())
    router.HandleNamed(route.DeviceIdentityName, "GET", route.DeviceIdentityPattern, handlersecure.MeHandler())

    if nil != instance.redisClient {
        instance.buildCatalogWriteThrottle()
    } else {
        instance.buildInProcessCatalogWriteThrottle(kernelInstance.Clock())
    }

    router.HandleNamed(route.LoginPageName, "GET", route.LoginPagePattern, handler.LoginPageHandler())

    secondFactorReplayGuard := instance.buildSecondFactorReplayGuard(kernelInstance.Clock())

    /* login-submit and logout are exposed to the route manifest (window.melodyRoutes) because the frontend resolves their URLs by name — the login form posts to route("example.login.submit") and the nav logs out via route("example.logout"); an unexposed route would make the client throw "unknown route". */
    /* the sign-in submit spends the same per-address budget as the nomenclature's writes: a password guessed in a loop is refused with 429 once the budget runs out, whichever replica each guess reaches */
    router.HandleWithOptions(
        route.LoginSubmitPattern,
        instance.throttledWrite(handler.LoginHandler(instance.buildLoginAuthentication(kernelInstance.Clock(), secondFactorReplayGuard))),
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

    /* the two doors resolve the store at each request (see two_factor.go), so they stand whenever the catalogue does: a store its migration refused answers 503 until it heals, rather than leaving the routes unregistered until the process restarts. The verification door burns an accepted code in the memory the sign-in reads, so a code is spent once across both. */
    if nil != instance.database {
        router.HandleNamed("example.twofactor.enroll", "POST", "/twofactor/enroll", handlertwofactor.EnrollHandler(twofactor.StoreFromRuntime))
        router.HandleNamed("example.twofactor.verify", "POST", "/twofactor/verify", handlertwofactor.VerifyHandler(twofactor.StoreFromRuntime, secondFactorReplayGuard))
    }

    /* the outbox handlers hold container.Lazy handles built at route-registration time: the store and relay services (provided by the outbox module's factories, see configure.go) are resolved at the first request, so registering the routes never touches the outbox schema or the transport. */
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
        router.HandleNamed("example.storage.link", "GET", "/storage/object/link", handlerstorage.LinkHandler(instance.storage))
    }

    router.HandleWithOptions(
        route.I18nGreetingPattern,
        handleri18n.GreetingHandler(),
        melodyhttp.NewRouteOptions(route.I18nGreetingName, []string{"GET"}, "", nil, nil, nil, route.I18nGreetingLocaleList(), 0, nil),
    )

    router.HandleNamed(route.EventsStreamName, "GET", route.EventsStreamPattern, handlerevent.StreamHandler())
    router.HandleNamed(route.EventsPublishName, "POST", route.EventsPublishPattern, handlerevent.PublishHandler(instance.messageBusDispatch))

    /* every catalog/user route below is exposed in the frontend zone: the admin SPA generates all of their URLs by name from the route manifest (data-route / route(...)), so an unexposed route would make the client throw "unknown route". */
    router.HandleWithOptions(route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler(), frontendRoute(route.CategoriesApiReadAllName, "GET"))
    router.HandleWithOptions(route.ReportsApiHistoryPattern, handlerreport.ApiHistoryHandler(), frontendRoute(route.ReportsApiHistoryName, "GET"))
    router.HandleWithOptions(route.ReportsApiExportPattern, handlerreport.ApiExportHandler(), frontendRoute(route.ReportsApiExportName, "GET"))

    instance.registerCurrencyApiRoutes(router)

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

/* frontendRoute marks a route as exposed in the frontend zone so its URL is generatable by name from the route manifest (window.melodyRoutes) that the admin SPA resolves data-route / route(...) calls against. */
/* registerCurrencyApiRoutes registers the four currency api doors through one route group, which prefixes their patterns and names; the writes sit behind the catalogue's write budget */
func (instance *Module) registerCurrencyApiRoutes(router melodyhttpcontract.Router) {
    currencyApi := router.Group(route.CurrenciesApiGroupPrefix)
    currencyApi.WithNamePrefix(route.CurrenciesApiGroupNamePrefix)
    currencyApi.HandleWithOptions(route.CurrenciesApiReadAllRelativePattern, handlercurrency.ApiReadAllHandler(), frontendRoute(route.CurrenciesApiReadAllRelativeName, "GET"))
    currencyApi.HandleWithOptions(route.CurrenciesApiCreateRelativePattern, instance.throttledWrite(handlercurrency.ApiCreateHandler()), frontendRoute(route.CurrenciesApiCreateRelativeName, "POST"))
    currencyApi.HandleWithOptions(route.CurrenciesApiUpdateRelativePattern, instance.throttledWrite(handlercurrency.ApiUpdateHandler()), frontendRoute(route.CurrenciesApiUpdateRelativeName, "PUT"))
    currencyApi.HandleWithOptions(route.CurrenciesApiDeleteRelativePattern, instance.throttledWrite(handlercurrency.ApiDeleteHandler()), frontendRoute(route.CurrenciesApiDeleteRelativeName, "DELETE"))
}

func frontendRoute(name string, method string) melodyhttpcontract.RouteOptions {
    return melodyhttp.NewRouteOptions(name, []string{method}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZoneFrontend))
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)

/* buildCatalogWriteThrottle prepares the shared budget the nomenclature's write endpoints sit behind.

   The counter lives in redis, so several replicas enforce one limit rather than each allowing its own, and the client key is resolved trusted-proxy-aware: behind the compose load balancer the X-Forwarded-For client is used, a direct hit falls back to the peer address, and a spoofed header from an untrusted peer is ignored. With redis unreachable the limiter fails closed, so a write is refused rather than let through uncounted. */
func (instance *Module) buildCatalogWriteThrottle() {
    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(
        melodyrueidis.NewRateLimiter(
            instance.redisClient,
            catalogWriteAllowance,
            catalogWriteWindow,
            melodyrueidis.WithRateLimiterKeyPrefix(redisRateLimitKeyPrefix),
        ),
        nil,
        nil,
    )

    rateLimitConfig.SetClientIpResolver(instance.trustedProxyResolver.Resolve)

    instance.catalogWriteThrottle = melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)
}

/* buildInProcessCatalogWriteThrottle arms the write budget of a process that runs without redis: a sliding window per client address held in this process, bounded in the addresses it tracks. It limits this process only — behind a balancer every replica would allow a budget of its own, which is why a deployment runs the redis budget — but a single process keeps its writes and its sign-ins throttled rather than unthrottled. */
func (instance *Module) buildInProcessCatalogWriteThrottle(clockInstance melodyclockcontract.Clock) {
    limiter := melodyhttpmiddleware.NewSlidingWindowLimiterWithClock(clockInstance, catalogWriteAllowance, catalogWriteWindow)
    limiter.SetMaxKeys(inProcessWriteThrottleMaxAddresses)

    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(limiter, nil, nil)
    rateLimitConfig.SetClientIpResolver(instance.trustedProxyResolver.Resolve)

    instance.catalogWriteThrottle = melodyhttpmiddleware.RateLimitMiddleware(rateLimitConfig)
}

/* throttledWrite puts an endpoint that changes the nomenclature, or signs a caller in, behind the per-address budget. The reads are left alone deliberately: a catalogue is meant to be browsed, and it is the writes that a runaway script turns into damage. The budget counts in redis when the example has one and in this process otherwise; a route registered before the budget is built is returned untouched. */
func (instance *Module) throttledWrite(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
    if nil == instance.catalogWriteThrottle {
        return next
    }

    return instance.catalogWriteThrottle(next)
}
