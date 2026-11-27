package config

import (
    "reflect"

    outboxintegration "github.com/precision-soft/melody/integrations/outbox/v4"
    "github.com/precision-soft/melody/v4/.example/handler"
    "github.com/precision-soft/melody/v4/.example/handler/accesstoken"
    handlercategory "github.com/precision-soft/melody/v4/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/v4/.example/handler/currency"
    handlerevent "github.com/precision-soft/melody/v4/.example/handler/event"
    handleri18n "github.com/precision-soft/melody/v4/.example/handler/i18n"
    handlerinternalauth "github.com/precision-soft/melody/v4/.example/handler/internalauth"
    handleroutbox "github.com/precision-soft/melody/v4/.example/handler/outbox"
    handlerproduct "github.com/precision-soft/melody/v4/.example/handler/product"
    handlerreport "github.com/precision-soft/melody/v4/.example/handler/report"
    handlersecure "github.com/precision-soft/melody/v4/.example/handler/secure"
    handlerstorage "github.com/precision-soft/melody/v4/.example/handler/storage"
    handlertwofactor "github.com/precision-soft/melody/v4/.example/handler/twofactor"
    handleruser "github.com/precision-soft/melody/v4/.example/handler/user"
    "github.com/precision-soft/melody/v4/.example/page"
    "github.com/precision-soft/melody/v4/.example/route"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    melodyopenapi "github.com/precision-soft/melody/v4/openapi"
    bun "github.com/uptrace/bun"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    melodystoragecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* StorageObjectMaxBodyBytes is the object upload's own body cap, 1 MiB, above the application's MELODY_HTTP_MAX_REQUEST_BODY_BYTES */
const StorageObjectMaxBodyBytes = 1 << 20

/* SecurityHeaders is the framework's default set with the pages' content security policy. */
func SecurityHeaders() melodyhttpmiddleware.SecurityHeadersConfig {
    return melodyhttpmiddleware.SecurityHeadersConfig{ContentSecurityPolicy: page.ContentSecurityPolicy}
}

func (instance *Module) RegisterHttpRoutes(kernelInstance melodykernelcontract.Kernel) {
    router := kernelInstance.HttpRouter()

    /* the health and openapi routes opt into the frontend route manifest (melody:routes:manifest) as working proof of the export: exposed + zoned public, so the TypeScript RouteGenerator can build their URLs by name */
    router.Handle(
        "/health",
        handler.HealthHandler(instance.applicationVersion),
        melodyhttp.RouteOptions{Name: "example.health", Methods: []string{"GET"}, Attributes: melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)},
    )

    router.Handle(
        "/openapi.json",
        melodyopenapi.SpecificationHandler(),
        melodyhttp.RouteOptions{Name: "example.openapi", Methods: []string{"GET"}, Attributes: melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)},
    )

    router.Handle("/platform/check", handler.PlatformCheckHandler(), melodyhttp.RouteOptions{Name: "example.platform.check", Methods: []string{"GET"}})

    /* the write budget is built before the first door it guards */
    if true == instance.redisConfigured() {
        instance.buildCatalogWriteThrottle()
    } else {
        instance.buildInProcessCatalogWriteThrottle(kernelInstance.Clock())
    }

    router.Handle("/messagebus/dispatch", instance.throttledWrite(handler.WelcomeEmailDispatchHandler("" != instance.parameterValue(parameterAmqpDsn))), melodyhttp.RouteOptions{Name: "example.messagebus.dispatch", Methods: []string{"POST"}})

    /* the example.metrics route is contributed by the opentelemetry module (see configure.go). */

    /* the /ws route is contributed by the websocket module (see configure.go). */

    router.Handle("/encrypt/roundtrip", handler.EncryptRoundTripHandler(instance.cipher), melodyhttp.RouteOptions{Name: "example.encrypt.roundtrip", Methods: []string{"GET"}})

    deviceIdentifierMaximumBytes := instance.deviceIdentifierMaximumBytes()
    melodyhttp.HandleJson(router, route.AccessTokenIssuePattern, accesstoken.Issue(deviceIdentifierMaximumBytes), melodyhttp.RouteOptions{Name: route.AccessTokenIssueName, Methods: []string{"POST"}}, melodyhttp.JsonHandlerConfig{})
    melodyhttp.HandleJson(router, route.AccessTokenRevokeDevicePattern, accesstoken.RevokeDevice, melodyhttp.RouteOptions{Name: route.AccessTokenRevokeDeviceName, Methods: []string{"POST"}}, melodyhttp.JsonHandlerConfig{})
    melodyhttp.HandleJson(router, route.AccessTokenRevokeUserPattern, accesstoken.RevokeUser, melodyhttp.RouteOptions{Name: route.AccessTokenRevokeUserName, Methods: []string{"POST"}}, melodyhttp.JsonHandlerConfig{})
    router.Handle(route.DeviceIdentityPattern, handlersecure.MeHandler(), melodyhttp.RouteOptions{Name: route.DeviceIdentityName, Methods: []string{"GET"}})

    router.Handle(route.LoginPagePattern, handler.LoginPageHandler(), melodyhttp.RouteOptions{Name: route.LoginPageName, Methods: []string{"GET"}})

    secondFactorReplayGuard := instance.buildSecondFactorReplayGuard(kernelInstance.Clock())

    /* the sign-in door is declared in RegisterSecurity, before the kernel-built parts of its chain exist, so they are handed over here */
    loginAuthentication := instance.buildLoginAuthentication(kernelInstance.Clock(), secondFactorReplayGuard)
    instance.loginDoorAuthenticator.Bind(loginAuthentication)

    /* handed over as the interface only when there is one, so a sign-in without a budget is no typed nil behind it */
    var secondFactorBudget handlertwofactor.FactorBudget
    if budget := loginAuthentication.Budget(); nil != budget {
        secondFactorBudget = budget
    }

    router.Handle(route.RoutesPattern, handler.RoutesHandler(), frontendRoute(route.RoutesName, "GET"))
    router.Handle(route.CataloguePagePattern, handler.CataloguePageHandler(), frontendRoute(route.CataloguePageName, "GET"))

    router.Handle(route.SecureMePattern, handlersecure.MeHandler(), melodyhttp.RouteOptions{Name: route.SecureMeName, Methods: []string{"GET"}})

    router.Handle(route.InternalWhoamiPattern, handlerinternalauth.WhoamiHandler(), melodyhttp.RouteOptions{Name: route.InternalWhoamiName, Methods: []string{"POST"}})

    /* the doors resolve the store at each request (see two_factor.go), so they stand whenever the catalogue does: a store its migration refused answers 503 until it heals, rather than leaving the routes unregistered until the process restarts. The verification door, and the enrollment door when it replaces an enrollment, burn an accepted code in the memory the sign-in reads, as does the confirmation door, so a code is spent once across all of them, and count every presented factor against the account's budget the sign-in spends. */
    if true == instance.catalogueWired {
        router.Handle(route.TwoFactorPagePattern, handlertwofactor.PageHandler(), frontendRoute(route.TwoFactorPageName, "GET"))
        router.Handle(route.TwoFactorEnrollPattern, handlertwofactor.EnrollHandler(twofactor.StoreFromRuntime, secondFactorReplayGuard, secondFactorBudget), frontendRoute(route.TwoFactorEnrollName, "POST"))
        router.Handle(route.TwoFactorConfirmPattern, handlertwofactor.ConfirmHandler(twofactor.StoreFromRuntime, secondFactorReplayGuard, secondFactorBudget), frontendRoute(route.TwoFactorConfirmName, "POST"))
        router.Handle(route.TwoFactorStatusPattern, handlertwofactor.StatusHandler(twofactor.StoreFromRuntime), frontendRoute(route.TwoFactorStatusName, "GET"))
        router.Handle(route.TwoFactorVerifyPattern, handlertwofactor.VerifyHandler(twofactor.StoreFromRuntime, secondFactorReplayGuard, secondFactorBudget), melodyhttp.RouteOptions{Name: route.TwoFactorVerifyName, Methods: []string{"POST"}})
    }

    /* the outbox handlers hold container.Lazy handles built at route-registration time: the store and relay services (provided by the outbox module's factories, see configure.go) are resolved at the first request, so registering the routes never touches the outbox schema or the transport. */
    if true == instance.catalogueWired {
        outboxStore := melodycontainer.Lazy[*outboxintegration.Store](kernelInstance.ServiceContainer(), outboxintegration.ServiceStore)
        outboxRelay := melodycontainer.Lazy[*outboxintegration.Relay](kernelInstance.ServiceContainer(), outboxintegration.ServiceRelay)

        catalogueDatabase := melodycontainer.Lazy[*bun.DB](kernelInstance.ServiceContainer(), serviceDatabase)

        router.Handle("/outbox/enqueue", instance.throttledWrite(handleroutbox.EnqueueHandler(catalogueDatabase, outboxStore)), melodyhttp.RouteOptions{Name: "example.outbox.enqueue", Methods: []string{"POST"}})
        router.Handle("/outbox/relay", instance.throttledWrite(handleroutbox.RelayHandler(outboxRelay)), melodyhttp.RouteOptions{Name: "example.outbox.relay", Methods: []string{"POST"}})
        router.Handle("/outbox/status", handleroutbox.StatusHandler(catalogueDatabase, outboxStore), melodyhttp.RouteOptions{Name: "example.outbox.status", Methods: []string{"GET"}})
    }

    if nil != instance.storageClient {
        /* the storage the aws.s3 module registers, checking its bucket, resolved at each request rather than captured, so the first upload after an outage checks the bucket again */
        objectStorage := melodycontainer.Lazy[melodystoragecontract.Storage](kernelInstance.ServiceContainer(), melodystorage.ServiceStorage)

        /* the one route whose body may outgrow the application's cap carries its own, so no other door is raised with it */
        router.Handle("/storage/object", instance.throttledWrite(handlerstorage.PutHandler(objectStorage)), melodyhttp.RouteOptions{Name: "example.storage.put", Methods: []string{"POST"}, Protection: melodyhttp.RouteProtectionConfig{MaxBodyBytes: melodybound.Of(StorageObjectMaxBodyBytes)}})
        router.Handle("/storage/object", handlerstorage.GetHandler(objectStorage), melodyhttp.RouteOptions{Name: "example.storage.get", Methods: []string{"GET"}})
        router.Handle("/storage/object/link", handlerstorage.LinkHandler(objectStorage), melodyhttp.RouteOptions{Name: "example.storage.link", Methods: []string{"GET"}})
    }

    router.Handle(
        route.I18nGreetingPattern,
        handleri18n.GreetingHandler(),
        melodyhttp.RouteOptions{Name: route.I18nGreetingName, Methods: []string{"GET"}, Locales: route.I18nGreetingLocaleList()},
    )

    router.Handle(route.EventsStreamPattern, handlerevent.StreamHandler(instance.eventStreamSlots), melodyhttp.RouteOptions{Name: route.EventsStreamName, Methods: []string{"GET"}})
    router.Handle(route.EventsPublishPattern, instance.throttledWrite(handlerevent.PublishHandler(instance.messageBusDispatch)), melodyhttp.RouteOptions{Name: route.EventsPublishName, Methods: []string{"POST"}})

    /* every catalog/user route below is exposed in the frontend zone: the admin SPA generates all of their URLs by name from the route manifest (data-route / route(...)), so an unexposed route would make the client throw "unknown route". */
    router.Handle(route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler(), frontendRoute(route.CategoriesApiReadAllName, "GET"))
    router.Handle(route.ReportsApiHistoryPattern, handlerreport.ApiHistoryHandler(), frontendRoute(route.ReportsApiHistoryName, "GET"))
    router.Handle(route.ReportsApiExportPattern, handlerreport.ApiExportHandler(), frontendRoute(route.ReportsApiExportName, "GET"))

    instance.registerCurrencyApiRoutes(router)

    router.Handle(route.ProductsListPagePattern, handlerproduct.ListPageHandler(), frontendRoute(route.ProductsListPageName, "GET"))
    router.Handle(route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler(), frontendRoute(route.ProductsCreatePageName, "GET"))
    router.Handle(route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler(), frontendRoute(route.ProductsUpdatePageName, "GET"))
    router.Handle(route.ProductsApiCreatePattern, instance.throttledWrite(handlerproduct.ApiCreateHandler()), frontendJsonRoute(route.ProductsApiCreateName, "POST", reflect.TypeFor[handlerproduct.CreateRequest]()))
    router.Handle(route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler(), frontendRoute(route.ProductsApiReadAllName, "GET"))
    router.Handle(route.ProductsApiReadPattern, handlerproduct.ApiReadHandler(), frontendRoute(route.ProductsApiReadName, "GET"))
    router.Handle(route.ProductsApiUpdatePattern, instance.throttledWrite(handlerproduct.ApiUpdateHandler()), frontendJsonRoute(route.ProductsApiUpdateName, "PUT", reflect.TypeFor[handlerproduct.UpdateRequest]()))
    router.Handle(route.ProductsApiDeletePattern, instance.throttledWrite(handlerproduct.ApiDeleteHandler()), frontendRoute(route.ProductsApiDeleteName, "DELETE"))

    router.Handle(route.UsersListPagePattern, handleruser.ListPageHandler(), frontendRoute(route.UsersListPageName, "GET"))
    router.Handle(route.UsersCreatePagePattern, handleruser.CreatePageHandler(), frontendRoute(route.UsersCreatePageName, "GET"))
    router.Handle(route.UsersUpdatePagePattern, handleruser.UpdatePageHandler(), frontendRoute(route.UsersUpdatePageName, "GET"))
    router.Handle(route.UsersApiCreatePattern, instance.throttledWrite(handleruser.ApiCreateHandler()), frontendRoute(route.UsersApiCreateName, "POST"))
    router.Handle(route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler(), frontendRoute(route.UsersApiReadAllName, "GET"))
    router.Handle(route.UsersApiReadPattern, handleruser.ApiReadHandler(), frontendRoute(route.UsersApiReadName, "GET"))
    router.Handle(route.UsersApiUpdatePattern, instance.throttledWrite(handleruser.ApiUpdateHandler()), frontendRoute(route.UsersApiUpdateName, "PUT"))
    router.Handle(route.UsersApiDeletePattern, instance.throttledWrite(handleruser.ApiDeleteHandler()), frontendRoute(route.UsersApiDeleteName, "DELETE"))
}

/* registerCurrencyApiRoutes registers the four currency api doors through one route group, which prefixes their patterns and names; the writes sit behind the catalogue's write budget */
func (instance *Module) registerCurrencyApiRoutes(router melodyhttpcontract.Router) {
    currencyApi := router.Group(
        melodyhttpcontract.GroupConfig{
            PathPrefix: route.CurrenciesApiGroupPrefix,
            NamePrefix: route.CurrenciesApiGroupNamePrefix,
        },
    )
    currencyApi.Handle(route.CurrenciesApiReadAllRelativePattern, handlercurrency.ApiReadAllHandler(), frontendRoute(route.CurrenciesApiReadAllRelativeName, "GET"))
    currencyApi.Handle(route.CurrenciesApiCreateRelativePattern, instance.throttledWrite(handlercurrency.ApiCreateHandler()), frontendJsonRoute(route.CurrenciesApiCreateRelativeName, "POST", reflect.TypeFor[handlercurrency.CreateRequest]()))
    currencyApi.Handle(route.CurrenciesApiUpdateRelativePattern, instance.throttledWrite(handlercurrency.ApiUpdateHandler()), frontendJsonRoute(route.CurrenciesApiUpdateRelativeName, "PUT", reflect.TypeFor[handlercurrency.UpdateRequest]()))
    currencyApi.Handle(route.CurrenciesApiDeleteRelativePattern, instance.throttledWrite(handlercurrency.ApiDeleteHandler()), frontendRoute(route.CurrenciesApiDeleteRelativeName, "DELETE"))
}

/* frontendRoute marks a route as exposed in the frontend zone so its URL is generatable by name from the route manifest (window.melodyRoutes) that the admin SPA resolves data-route / route(...) calls against. */
func frontendRoute(name string, method string) melodyhttpcontract.RouteOptions {
    return melodyhttp.RouteOptions{Name: name, Methods: []string{method}, Attributes: melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZoneFrontend)}
}

/* frontendJsonRoute declares the body type a json handler wrapped behind its role check binds, so the boot verifies its validate tags */
func frontendJsonRoute(name string, method string, requestType reflect.Type) melodyhttpcontract.RouteOptions {
    options := frontendRoute(name, method)
    options.RequestType = requestType

    return options
}

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)

/* buildCatalogWriteThrottle prepares the shared budget the nomenclature's write endpoints sit behind. The counter lives in redis, so several replicas enforce one limit rather than each allowing its own, and the client key is resolved trusted-proxy-aware: behind the compose load balancer the X-Forwarded-For client is used, a direct hit falls back to the peer address, and a spoofed header from an untrusted peer is ignored. With redis unreachable the limiter fails closed, so a write is refused rather than let through uncounted. */
func (instance *Module) buildCatalogWriteThrottle() {
    limiter := &runtimeResolvedRateLimiter{serviceName: serviceCatalogWriteLimiter}

    instance.loginDoorLimiter.bind(limiter)

    instance.catalogWriteThrottle = melodyhttpmiddleware.RateLimitMiddleware(limiter, melodyhttpmiddleware.RateLimitConfig{
        ClientIpResolver: instance.trustedProxyResolver.Resolve,
    })
}

/* buildInProcessCatalogWriteThrottle arms the write budget of a process that runs without redis: a sliding window per client address held in this process, bounded in the addresses it tracks. It limits this process only — behind a balancer every replica would allow a budget of its own, which is why a deployment runs the redis budget — but a single process keeps its writes and its sign-ins throttled rather than unthrottled. Past 10,000 addresses an unseen address takes the slot of the address seen longest ago, which comes back with a full budget: a caller varying its address gains nothing a fresh address does not already give it; the redis budget has no such table. */
func (instance *Module) buildInProcessCatalogWriteThrottle(clockInstance melodyclockcontract.Clock) {
    limiter := melodyhttpmiddleware.NewSlidingWindowLimiter(catalogWriteLimiterName, catalogWriteAllowance, catalogWriteWindow, melodyhttpmiddleware.LimiterConfig{
        MaxKeys: melodybound.Of(inProcessWriteThrottleMaxAddresses),
        Clock:   clockInstance,
    })

    instance.loginDoorLimiter.bind(limiter)

    instance.catalogWriteThrottle = melodyhttpmiddleware.RateLimitMiddleware(limiter, melodyhttpmiddleware.RateLimitConfig{
        ClientIpResolver: instance.trustedProxyResolver.Resolve,
    })
}

/* throttledWrite puts an endpoint that changes the nomenclature, or spends a backend this process does not own (the message bus, the outbox, the object store, the event hub), behind the per-address budget. The reads are left alone deliberately: a catalogue is meant to be browsed, and it is the writes that a runaway script turns into damage. The budget counts in redis when the example has one and in this process otherwise; a route registered before the budget is built is returned untouched. */
func (instance *Module) throttledWrite(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
    if nil == instance.catalogWriteThrottle {
        return next
    }

    return instance.catalogWriteThrottle(next)
}

/* lateBoundRateLimiter is the write budget as the sign-in door holds it: the door is declared before the budget is built, and it charges the same counter as the routes behind throttledWrite. */
type lateBoundRateLimiter struct {
    limiter melodyhttpcontract.RateLimiter
}

func (instance *lateBoundRateLimiter) bind(limiter melodyhttpcontract.RateLimiter) {
    instance.limiter = limiter
}

func (instance *lateBoundRateLimiter) Allow(runtimeInstance melodycontainercontract.Runtime, key string) (melodyhttpcontract.RateLimitDecision, error) {
    return instance.limiter.Allow(runtimeInstance, key)
}

func (instance *lateBoundRateLimiter) Reset(runtimeInstance melodycontainercontract.Runtime, key string) error {
    return instance.limiter.Reset(runtimeInstance, key)
}

var _ melodyhttpcontract.RateLimiter = (*lateBoundRateLimiter)(nil)
