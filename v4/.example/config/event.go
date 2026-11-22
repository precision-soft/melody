package config

import (
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/route"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcors "github.com/precision-soft/melody/v4/http/cors"
    melodyhttpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

const apiPathSegment = "/api/"

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    instance.registerSubscribers(kernelInstance.EventDispatcher())
}

/* registerSubscribers installs the application's subscribers on the dispatcher; a door of its own so what the composition root INSTALLS can be read off a dispatcher, not only what each subscriber does once installed. */
func (instance *Module) registerSubscribers(eventDispatcher melodyeventcontract.EventDispatcher) {
    /* the deletion's owner declares which of its listeners still run after a failure, ahead of the subscribers carrying the tags */
    eventDispatcher.DeclareEvent(event.UserDeletedEventName, event.UserDeletedEventConfig())

    instance.registerRequiredRequestContextListener(eventDispatcher)
    instance.registerApiExceptionListener(eventDispatcher)

    /* these subscribers live for the process, so the registrations AddSubscriber answers are not kept; an application that removes a subscriber at runtime keeps its registration and hands it to RemoveSubscriber. */
    eventDispatcher.AddSubscriber(
        subscriber.NewProductEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCategoryEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCurrencyEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewUserEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewSecurityAuthenticationEventSubscriber(),
    )

    /* the tokens go with the account, from the store the token firewalls read: redis when wired, the process's own otherwise. The release carries the release tag the deletion declares, so no failure of a listener ahead of it skips it; an outage of its own store fails the deletion loudly, the two-factor release and the cache drop behind it still run, and the device firewall refuses the tokens it left, its account being gone */
    eventDispatcher.AddSubscriber(
        subscriber.NewAccessTokenReleaseSubscriber(func(runtimeInstance melodycontainercontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
            return examplesecurity.TokenStoreFromRuntime(runtimeInstance).TokenStore()
        }),
    )

    /* without a database there is no enrollment to release. The release resolves the store at each deletion and journals a store it cannot reach rather than failing the deletion, since the cascade on the account releases the row; it runs after the token release and ahead of the cache subscriber */
    if true == instance.catalogueWired {
        eventDispatcher.AddSubscriber(
            subscriber.NewTwoFactorEnrollmentSubscriber(twofactor.StoreFromRuntime),
        )
    }

    instance.registerCorsListeners(eventDispatcher)
    instance.registerRateLimitRequestListener(eventDispatcher)
}

/* registerCorsListeners wires cors as listeners rather than as the middleware: a preflight to an access-controlled path and the security listeners' refusals never enter the middleware chain, and the request listener runs ahead of token resolution. An empty value leaves cors unwired, since cors.NewService over an empty list would deny every origin. */
func (instance *Module) registerCorsListeners(eventDispatcher melodyeventcontract.EventDispatcher) {
    originList := make([]string, 0)

    for _, origin := range strings.Split(instance.parameterValue(parameterCorsAllowOrigins), ",") {
        trimmed := strings.TrimSpace(origin)
        if "" == trimmed {
            continue
        }

        originList = append(originList, trimmed)
    }

    if 0 == len(originList) {
        return
    }

    corsService := melodyhttpcors.NewService(melodyhttpcors.Config{
        AllowOrigins: originList,
        AllowHeaders: []string{"Content-Type", "X-Api-Key"},
        MaxAge:       melodybound.Of(600),
    })

    melodyhttpcors.RegisterListeners(eventDispatcher, corsService)
}

/* registerRateLimitRequestListener meters every request on kernel.request, ahead of authentication and access control, so a burst the security chain refuses is charged too; the write throttle in http.go meters only the handler path. The budget is per client address per hour, and an unset value leaves it unwired. */
func (instance *Module) registerRateLimitRequestListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    resolved, resolveErr := melodyconfig.ResolveLimitParameter(
        instance.configuration.MustGet(parameterRequestBudgetPerHour),
        environmentKeyRequestBudgetPerHour,
        requestBudgetPerHourRule,
    )
    if nil != resolveErr {
        melodyexception.Panic(melodyexception.FromError(resolveErr))
    }

    /* the rule's default, zero, is the unset budget: the door stays unwired */
    if 0 == resolved.Value {
        return
    }

    melodyhttpmiddleware.RegisterRateLimitRequestListener(eventDispatcher, requestBudgetLimiter(resolved.Value), requestBudgetConfig(instance.trustedProxyResolver))
}

var requestBudgetPerHourRule = melodybound.Rule[int]{Minimum: 1}

/* requestBudgetWindow is the window the request budget is counted over: the switch names a budget per hour */
const requestBudgetWindow = time.Hour

/* requestBudgetLimiter is the hourly budget, held in this process and tracking at most 10,000 addresses, the ceiling of the example's other in-process tables; past it an unseen address takes the slot of the address whose last request is the oldest, which comes back with a full budget. */
func requestBudgetLimiter(budget int) *melodyhttpmiddleware.FixedWindowLimiter {
    return melodyhttpmiddleware.NewFixedWindowLimiter("request_budget", budget, requestBudgetWindow, melodyhttpmiddleware.LimiterConfig{
        MaxKeys: melodybound.Of(inProcessWriteThrottleMaxAddresses),
    })
}

/* requestBudgetConfig keys the budget through the trusted-proxy resolver the write throttle uses: on the peer address alone every client behind the balancer shares one key, and a header believed from anywhere would let a neighbouring process choose the key it is charged to. */
func requestBudgetConfig(trustedProxyResolver *trustedProxyResolver) melodyhttpmiddleware.RateLimitConfig {
    return melodyhttpmiddleware.RateLimitConfig{
        ClientIpResolver: trustedProxyResolver.Resolve,
    }
}

/* registerRequiredRequestContextListener registers a required kernel.request listener that prepares a per-request attribute later stages depend on; the kernel fails closed if another kernel.request listener stops propagation before it. */
func (instance *Module) registerRequiredRequestContextListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    eventDispatcher.AddListener(
        melodykernelcontract.EventKernelRequest,
        func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*melodyhttp.KernelRequestEvent)
            if false == ok || nil == requestEvent || nil == requestEvent.Request() {
                return nil
            }

            requestEvent.Request().Attributes().Set("example.requestContextReady", true)

            return nil
        },
        melodyeventcontract.ListenerConfig{Priority: 10, Required: true},
    )
}

/* registerApiExceptionListener answers an error under the api paths and on the sign-in door, which the page reads as json, with the example's json envelope, the refusals of the security chain included; declining leaves every other path to the framework's built-in listener. */
func (instance *Module) registerApiExceptionListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    eventDispatcher.AddListener(
        melodykernelcontract.EventKernelException,
        func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
            exceptionEvent, ok := eventValue.Payload().(*melodyhttp.KernelExceptionEvent)
            if false == ok || nil == exceptionEvent || nil == exceptionEvent.Request() {
                return nil
            }

            requestUrl := exceptionEvent.Request().HttpRequest().URL
            if false == strings.Contains(requestUrl.Path, apiPathSegment) && melodysecurity.FirewallDoorPathForm(route.LoginDoorPattern) != melodysecurity.FirewallDoorPathForm(melodyhttp.RequestPathAsRouted(requestUrl.EscapedPath())) {
                return nil
            }

            exceptionEvent.SetResponse(presenter.ApiRefusalOfError(runtimeInstance, exceptionEvent.Request(), exceptionEvent.Err(), exceptionEvent.UnavailableRetryAfter()))

            return nil
        },
        melodyeventcontract.ListenerConfig{},
    )
}

var _ melodyapplicationcontract.EventModule = (*Module)(nil)
