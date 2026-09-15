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

    /* RequestListenerPriority places the preflight answer ahead of the security chain: token resolution listens at 50 and access control at 20, and a preflight carries neither cookie nor Authorization by specification, so behind them it could only be refused. */
    RequestListenerPriority = 100
)

/* RegisterListeners installs preflight handling before security and response decoration for all kernel responses, including listener-produced refusals and errors. */
func RegisterListeners(eventDispatcher eventcontract.EventDispatcher, service *Service) {
    if nil == service {
        service = DefaultService()
    }

    RegisterRequestListener(eventDispatcher, service)
    RegisterResponseListener(eventDispatcher, service)
}

/* RegisterRequestListener handles valid preflights from allowed origins before security. Disallowed origins and non-preflight requests fall through. An already-answered allowed preflight retains its status and gains CORS headers. */
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
