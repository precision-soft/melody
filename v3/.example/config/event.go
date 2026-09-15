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
    instance.registerSubscribers(kernelInstance.EventDispatcher())
}

func (instance *Module) registerSubscribers(eventDispatcher melodyeventcontract.EventDispatcher) {
    instance.registerRequiredRequestContextListener(eventDispatcher)

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
        subscriber.NewUserEventSubscriberWithEnrollmentStore(instance.twoFactorStore),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewSecurityAuthenticationEventSubscriber(),
    )

    instance.registerCorsListeners(eventDispatcher)
    instance.registerRateLimitRequestListener(eventDispatcher)
}

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

func (instance *Module) registerRateLimitRequestListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    budgetValue := strings.TrimSpace(instance.environmentValue(environmentKeyRequestBudgetPerHour))
    if "" == budgetValue {
        return
    }

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

func requestBudgetConfig(budget int, trustedProxyResolver *trustedProxyResolver) *melodyhttpmiddleware.RateLimitConfig {
    rateLimitConfig := melodyhttpmiddleware.NewRateLimitConfig(
        melodyhttpmiddleware.NewFixedWindowLimiter(budget, time.Hour),
        nil,
        nil,
    )

    rateLimitConfig.SetClientIpResolver(trustedProxyResolver.Resolve)

    return rateLimitConfig
}

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
