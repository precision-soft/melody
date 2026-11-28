package http

import (
    "github.com/precision-soft/melody/v4/internal"

    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

const (
    KernelHttpProfilerListenerPriority = -900
)

func RegisterKernelHttpProfilerListener(eventDispatcher eventcontract.EventDispatcher) {
    eventDispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            if nil == responseEvent {
                return nil
            }

            if true == internal.IsNilInterface(responseEvent.Request()) {
                return nil
            }

            requestContext := responseEvent.Request().RequestContext()
            if nil == requestContext {
                return nil
            }

            routeName := ""
            routePattern := ""

            /* the application's attribute bag may be a typed nil, read through the interface as router_utility reads it */
            if false == internal.IsNilInterface(responseEvent.Request().Attributes()) {
                routeNameValue, exists := responseEvent.Request().Attributes().Get(RouteAttributeName)
                if true == exists {
                    routeName, _ = routeNameValue.(string)
                }

                routePatternValue, exists := responseEvent.Request().Attributes().Get(RouteAttributePattern)
                if true == exists {
                    routePattern, _ = routePatternValue.(string)
                }
            }

            finishedAt := time.Now()
            duration := finishedAt.Sub(requestContext.StartedAt())

            statusCode := 0
            if nil != responseEvent.Response() {
                statusCode = responseEvent.Response().StatusCode()
            }

            method := ""
            path := ""

            if nil != responseEvent.Request().HttpRequest() {
                method = responseEvent.Request().HttpRequest().Method
                if nil != responseEvent.Request().HttpRequest().URL {
                    path = responseEvent.Request().HttpRequest().URL.Path
                }
            }

            profile := newHttpRequestProfile(
                requestContext.RequestId(),
                method,
                path,
                routeName,
                routePattern,
                statusCode,
                requestContext.StartedAt(),
                finishedAt,
                duration,
            )

            _, err := eventDispatcher.DispatchName(runtimeInstance, httpcontract.EventHttpRequestProfile, profile)

            return err
        },
        eventcontract.ListenerConfig{Priority: KernelHttpProfilerListenerPriority},
    )
}
