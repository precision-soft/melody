package cors

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ResponseListenerPriority = -100

    /* RequestListenerPriority places the preflight answer ahead of token resolution (50) and access control (20), since a preflight carries no credentials by specification. */
    RequestListenerPriority = 100
)

/* RegisterListeners wires both cors doors: the request listener that answers a preflight before the security chain, and the response listener that decorates every response, security refusals and error pages included. The Middleware decorates the handler path inside the chain. */
func RegisterListeners(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    RegisterRequestListener(eventDispatcher, service)
    RegisterResponseListener(eventDispatcher, service)
}

/* RegisterRequestListener answers a well-formed preflight from an allowed origin before the security chain runs; anything else falls through untouched, so it never widens what security refuses. A preflight an earlier listener already answered keeps its status and is decorated with the cross-origin headers. */
func RegisterRequestListener(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent || true == internal.IsNilInterface(requestEvent.Request()) {
                return nil
            }

            origin := service.RequestOrigin(requestEvent.Request())
            if "" == origin || false == service.OriginAllowed(origin) {
                return nil
            }

            if false == service.IsPreflight(requestEvent.Request()) {
                return nil
            }

            /* an earlier listener, the rate limiter among them, answered the preflight: its refusal is decorated so the browser can read it, and its status is left untouched */
            if existingResponse := requestEvent.Response(); nil != existingResponse {
                if nil == existingResponse.Headers() {
                    existingResponse.SetHeaders(make(nethttp.Header))
                }

                service.ApplyPreflightHeaders(origin, existingResponse.Headers())

                return nil
            }

            response := http.EmptyResponse(nethttp.StatusNoContent)
            service.ApplyPreflightHeaders(origin, response.Headers())

            requestEvent.SetResponse(response)

            return nil
        },
        RequestListenerPriority,
    )
}

/* RegisterResponseListener decorates the response the kernel is about to write. A handler that committed its own headers before returning, a stream or a long poll, is past its reach and needs the Middleware. */
func RegisterResponseListener(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    eventDispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
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

            if false == service.OriginAllowed(origin) {
                return nil
            }

            service.ApplyResponseHeaders(origin, response.Headers())

            return nil
        },
        ResponseListenerPriority,
    )
}
