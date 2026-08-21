package cors

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/http"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ResponseListenerPriority = -100

    /* RequestListenerPriority places the preflight answer ahead of the security chain: token resolution listens at 50 and access control at 20, and a preflight carries neither cookie nor Authorization by specification, so behind them it could only be refused. */
    RequestListenerPriority = 100
)

/* RegisterListeners wires both cors doors at once: the request listener that answers a preflight before the security chain can refuse it, and the response listener that decorates every response — the security refusals and the error pages the middleware chain never sees included. An application serving cross-origin traffic registers these; the Middleware remains for decorating the handler path inside the chain. */
func RegisterListeners(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    RegisterRequestListener(eventDispatcher, service)
    RegisterResponseListener(eventDispatcher, service)
}

/* RegisterRequestListener answers a well-formed preflight from an allowed origin before the security chain runs. A disallowed or absent origin and an OPTIONS request that is not a preflight fall through untouched, so the listener can only answer what the cors middleware would have answered had the request reached it, never widen what security refuses. A preflight from an allowed origin that a listener ahead of this one already answered is the one case that is neither answered nor left alone: its STATUS is untouched, and it is decorated with the cross-origin headers for the reason spelled out at that branch. */
func RegisterRequestListener(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    eventDispatcher.AddListener(
        kernelcontract.EventKernelRequest,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
            if false == ok || nil == requestEvent || nil == requestEvent.Request() {
                return nil
            }

            origin := service.RequestOrigin(requestEvent.Request())
            if "" == origin || false == service.OriginAllowed(origin) {
                return nil
            }

            if false == service.IsPreflight(requestEvent.Request()) {
                return nil
            }

            /* a listener ahead of this one already answered the preflight — the rate limiter at priority 200 answering a 429 among them. A browser reads a preflight refusal only if it carries the cross-origin headers, so decorate that refusal with them rather than leaving it opaque and letting the page report a bare network failure it cannot distinguish from a rate limit; the status the earlier listener chose is left untouched. */
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
