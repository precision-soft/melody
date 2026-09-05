package config

import (
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcors "github.com/precision-soft/melody/v3/http/cors"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    eventDispatcher := kernelInstance.EventDispatcher()

    instance.registerRequiredRequestContextListener(eventDispatcher)

    /* the registration each call answers is deliberately discarded: these five are installed once at boot and live for the process, so nothing here ever removes them. An application that removes a subscriber at runtime must keep what AddSubscriber returns — the subscriber value is not accepted back. */
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

    instance.registerCorsListeners(eventDispatcher)
    instance.registerRateLimitRequestListener(eventDispatcher)
}

/* registerCorsListeners wires cors as LISTENERS rather than as the middleware: a preflight aimed at an access-controlled path and the refusals the security listeners produce never enter the middleware chain, and the request listener sits ahead of token resolution so a preflight is answered before anything can refuse it. The empty value keeps the door unwired, like every other switch of the example — handing cors.NewService an empty list would deny every origin, which is a different statement than "no cors at all". */
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

/* registerRateLimitRequestListener meters every request on kernel.request, ahead of authentication and access control — the catalogue write throttle in http.go meters only what reaches the handler path, so a burst the security chain refuses consumes no budget there; this door charges that burst. The budget is per client address and per hour, generous enough that the browsing the example invites never meets it, and the unset value keeps the door unwired like every other switch of the example. */
func (instance *Module) registerRateLimitRequestListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    budgetValue := strings.TrimSpace(instance.environmentValue(environmentKeyRequestBudgetPerHour))
    if "" == budgetValue {
        return
    }

    /* a malformed value is refused by name rather than read as unset: swallowed, a typo in the key disarmed the global budget with no signal on any channel, indistinguishable from never having asked — the cron heartbeat opt-in refuses its malformed value for the same reason */
    budget, parseErr := strconv.Atoi(budgetValue)
    if nil != parseErr || 0 >= budget {
        melodyexception.Panic(melodyexception.NewError(
            "the request budget switch does not hold a positive integer; unset it to keep the door unwired",
            map[string]any{"key": environmentKeyRequestBudgetPerHour, "value": budgetValue},
            parseErr,
        ))
    }

    melodyhttpmiddleware.RegisterRateLimitRequestListener(
        eventDispatcher,
        melodyhttpmiddleware.NewRateLimitConfig(
            melodyhttpmiddleware.NewFixedWindowLimiter(budget, time.Hour),
            nil,
            nil,
        ),
    )
}

/* registerRequiredRequestContextListener demonstrates a required kernel.request listener. It prepares a per-request attribute that later stages depend on, so it must always run; marking it required through the event RequiredListenerRegistrar makes the kernel fail closed if any other kernel.request listener stops propagation before it — the same guarantee the security access-control listener gets automatically. A listener that deliberately short-circuits the request phase past required listeners would instead opt out with eventDispatcher.MarkListenerMaySkipRequiredListeners(registration). */
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
