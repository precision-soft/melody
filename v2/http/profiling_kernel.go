package http

import (
    "time"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

            if nil == responseEvent.Request() {
                return nil
            }

            requestContext := (httpcontract.RequestContext)(nil)

            if nil != responseEvent.Request().Attributes() {
                requestContext = responseEvent.Request().RequestContext()
            }

            if nil == requestContext {
                return nil
            }

            routeName := ""
            routePattern := ""

            if nil != responseEvent.Request().Attributes() {
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
