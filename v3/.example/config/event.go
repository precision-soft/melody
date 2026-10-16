package config

import (
    "strconv"
    "strings"
    "time"

    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcors "github.com/precision-soft/melody/v3/http/cors"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    instance.registerSubscribers(kernelInstance.EventDispatcher())
}

/* registerSubscribers installs the application's subscribers on the dispatcher; a door of its own so what the composition root INSTALLS can be read off a dispatcher, not only what each subscriber does once installed. */
func (instance *Module) registerSubscribers(eventDispatcher melodyeventcontract.EventDispatcher) {
    instance.registerRequiredRequestContextListener(eventDispatcher)

    /* these subscribers live for the process; an application that removes a subscriber at runtime hands it back to RemoveSubscriber, or installs it through SubscriberRegistrar.AddSubscriberWithRegistration and keeps the registration. */
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

    /* the tokens go with the account, from the store the token firewalls read: redis when wired, the process's own otherwise. The release outranks every other listener of the deletion, so no outage of theirs skips it; an outage of its own store fails the deletion loudly, and the device firewall refuses the tokens it left, its account being gone */
    eventDispatcher.AddSubscriber(
        subscriber.NewAccessTokenReleaseSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) melodysecuritycontract.EpochRevocableTokenStore {
            return examplesecurity.TokenStoreFromResolver(runtimeInstance.Container())
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

    for _, origin := range strings.Split(instance.environmentValue(environmentKeyCorsAllowOrigins), ",") {
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
        MaxAge:       600,
    })

    melodyhttpcors.RegisterListeners(eventDispatcher, corsService)
}

/* registerRateLimitRequestListener meters every request on kernel.request, ahead of authentication and access control, so a burst the security chain refuses is charged too; the write throttle in http.go meters only the handler path. The budget is per client address per hour, and an unset value leaves it unwired. */
func (instance *Module) registerRateLimitRequestListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    budgetValue := strings.TrimSpace(instance.environmentValue(environmentKeyRequestBudgetPerHour))
    if "" == budgetValue {
        return
    }

    /* a malformed value is refused by name rather than read as unset, because a typo would otherwise disarm the budget with no signal */
    budget, parseErr := strconv.Atoi(budgetValue)
    if nil != parseErr || 0 >= budget {
        melodyexception.Panic(melodyexception.NewError(
            "the request budget switch does not hold a positive integer; unset it to keep the door unwired",
            map[string]any{"key": environmentKeyRequestBudgetPerHour, "value": budgetValue},
            parseErr,
        ))
    }

    melodyhttpmiddleware.RegisterRateLimitRequestListener(eventDispatcher, requestBudgetConfig(budget, instance.trustedProxyResolver))
}

/* requestBudgetWindow is the window the request budget is counted over: the switch names a budget per hour */
const requestBudgetWindow = time.Hour

/* requestBudgetConfig is the hourly budget, keyed through the trusted-proxy resolver the write throttle uses: on the peer address alone every client behind the balancer shares one key, and a header believed from anywhere would let a neighbouring process choose the key it is charged to. The budget is held in this process and tracks at most 10,000 addresses, the ceiling of the example's other in-process tables; past it an unseen address takes the slot of the address whose last request is the oldest, which comes back with a full budget. Refusing it instead would let 10,000 addresses lock every new visitor out for the two hours an idle bucket is held over an hourly window. */
func requestBudgetConfig(budget int, trustedProxyResolver *trustedProxyResolver) *melodyhttpmiddleware.RateLimitConfig {
    limiter := melodyhttpmiddleware.NewFixedWindowLimiter(budget, requestBudgetWindow)
    limiter.SetMaxKeys(inProcessWriteThrottleMaxAddresses)
    limiter.SetEvictOldestKeyAtCeiling(true)

    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(
        limiter,
        nil,
        nil,
    )

    rateLimitConfig.SetClientIpResolver(trustedProxyResolver.Resolve)

    return rateLimitConfig
}

/* registerRequiredRequestContextListener registers a required kernel.request listener that prepares a per-request attribute later stages depend on; the kernel fails closed if another kernel.request listener stops propagation before it. */
func (instance *Module) registerRequiredRequestContextListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    registration := eventDispatcher.AddListener(
        melodykernelcontract.EventKernelRequest,
        func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*melodyhttp.KernelRequestEvent)
            if false == ok || nil == requestEvent || nil == requestEvent.Request() {
                return nil
            }

            requestEvent.Request().Attributes().Set("example.requestContextReady", true)

            return nil
        },
        10,
    )

    if registrar, ok := eventDispatcher.(melodyeventcontract.RequiredListenerRegistrar); true == ok {
        registrar.MarkListenerRequired(registration)
    }
}

var _ melodyapplicationcontract.EventModule = (*Module)(nil)
