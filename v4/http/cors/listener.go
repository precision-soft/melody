package cors

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

const (
    ResponseListenerPriority = -100

    /* RequestListenerPriority places the preflight answer ahead of token resolution (50) and access control (20), since a preflight carries no credentials by specification. */
    RequestListenerPriority = 100
)

/* RegisterListeners wires both cors doors: the request listener that answers a preflight before the security chain, and the response listener that decorates every response the kernel publishes through kernel.response, security refusals and error pages included; the 500 the write step substitutes after that publish, for a non-zero status outside [100, 999] or a lost session write, and the 500 the outermost guard writes for a panic past the recovery go out undecorated. The response listener carries the melody.cors tag, which the application declares on kernel.response, so it still decorates the response when a listener above it failed, adding headers only: the status and the body are never touched and no preflight is answered after a failure; on a dispatcher that declares nothing, such a response goes out undecorated. The Middleware decorates the handler path inside the chain. */
func RegisterListeners(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    requireService(service, "cors.RegisterListeners")

    RegisterRequestListener(eventDispatcher, service)
    RegisterResponseListener(eventDispatcher, service)
}

/* RegisterRequestListener answers a well-formed preflight from an allowed origin before the security chain runs, which ends the kernel.request dispatch; anything else falls through untouched, so it never widens what security refuses. It may skip the required listeners behind it, since a preflight runs no handler. */
func RegisterRequestListener(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    requireService(service, "cors.RegisterRequestListener")

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            origin := service.RequestOrigin(requestEvent.Request())
            if "" == origin || false == service.OriginAllowed(requestEvent.Request(), origin) {
                return nil
            }

            if false == service.IsPreflight(requestEvent.Request()) {
                return nil
            }

            response := http.EmptyResponse(nethttp.StatusNoContent)
            service.ApplyPreflightHeaders(origin, response.Headers())

            requestEvent.SetResponse(response)

            return nil
        },
        eventcontract.ListenerConfig{Priority: RequestListenerPriority, Failure: eventcontract.ListenerFailureConfig{MaySkipRequiredListeners: true}},
    )
}

/* RegisterResponseListener decorates the response the kernel is about to write, a preflight's with the preflight headers too. A handler that committed its own headers before returning, a stream or a long poll, is past its reach and needs the Middleware. */
func RegisterResponseListener(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    requireService(service, "cors.RegisterResponseListener")

    eventDispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*http.KernelResponseEvent)
            if false == ok || nil == responseEvent {
                return nil
            }

            response := responseEvent.Response()
            if nil == response {
                return nil
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            /* emitted on every path so a shared cache cannot serve an origin-less body to an allowed origin */
            addVaryOrigin(response.Headers())

            origin := service.RequestOrigin(responseEvent.Request())
            if "" == origin {
                return nil
            }

            if false == service.OriginAllowed(responseEvent.Request(), origin) {
                return nil
            }

            /* a preflight answered or refused by a listener ahead of the request listener, the rate limiter among them, carries the preflight headers so the browser can read its answer; the status and the body are left untouched */
            if true == service.IsPreflight(responseEvent.Request()) {
                service.ApplyPreflightHeaders(origin, response.Headers())
            }

            service.ApplyResponseHeaders(origin, response.Headers())

            return nil
        },
        eventcontract.ListenerConfig{Priority: ResponseListenerPriority, Tags: []string{kernelcontract.ListenerTagKernelCors}},
    )
}
