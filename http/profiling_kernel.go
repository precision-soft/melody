package http

import (
    "github.com/precision-soft/melody/internal"

    "time"

    eventcontract "github.com/precision-soft/melody/event/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    EventHttpRequestProfile = "http.request.profile"

    KernelHttpProfilerListenerPriority = -900
)

func RegisterKernelHttpProfilerListener(eventDispatcher eventcontract.EventDispatcher) {
    eventDispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
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

            /* the request is the application's, and its Attributes() returns an interface: a nil pointer
            of the application's own bag type reads as non-nil against a bare comparison and dereferences
            its nil receiver inside Get, in a response listener no recover covers. router_utility reads
            the same value the same way. */
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

            profile := NewHttpRequestProfile(
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

            _, err := eventDispatcher.DispatchName(runtimeInstance, EventHttpRequestProfile, profile)

            return err
        },
        KernelHttpProfilerListenerPriority,
    )
}
