package config

import (
    "strings"

    "github.com/precision-soft/melody/.example/subscriber"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodyhttpcors "github.com/precision-soft/melody/http/cors"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    eventDispatcher := kernelInstance.EventDispatcher()

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

    instance.registerCorsListeners(kernelInstance)
}

/* registerCorsListeners wires cors as LISTENERS rather than as the middleware: a preflight aimed at an access-controlled path and the refusals the security listeners produce never enter the middleware chain, and the request listener sits ahead of token resolution so a preflight is answered before anything can refuse it. */
func (instance *Module) registerCorsListeners(kernelInstance melodykernelcontract.Kernel) {
    corsService := corsServiceFromOrigins(parameterValue(kernelInstance, ParameterCorsAllowOrigins))
    if nil == corsService {
        return
    }

    melodyhttpcors.RegisterListeners(kernelInstance.EventDispatcher(), corsService)
}

/* corsServiceFromOrigins folds the comma-separated env value into the declared origins, dropping the empty entries a trailing comma leaves, and answers nil when none survive: handing cors.NewService an empty list would deny every origin, which is a different statement than "no cors at all" — the empty value keeps the door unwired, like every other switch of the example. */
func corsServiceFromOrigins(value string) *melodyhttpcors.Service {
    originList := make([]string, 0)

    for _, origin := range strings.Split(value, ",") {
        trimmed := strings.TrimSpace(origin)
        if "" == trimmed {
            continue
        }

        originList = append(originList, trimmed)
    }

    if 0 == len(originList) {
        return nil
    }

    return melodyhttpcors.NewService(melodyhttpcors.Config{
        AllowOrigins: originList,
        AllowHeaders: []string{"Content-Type", "X-Api-Key"},
        MaxAge:       600,
    })
}

var _ melodyapplicationcontract.EventModule = (*Module)(nil)
