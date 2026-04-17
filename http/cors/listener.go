package cors

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/http"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    ResponseListenerPriority = -100
)

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

            request := responseEvent.Request()
            origin := service.RequestOrigin(request)
            if "" == origin {
                return nil
            }

            if false == service.OriginAllowed(origin) {
                return nil
            }

            response := responseEvent.Response()
            if nil == response {
                return nil
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            service.ApplyResponseHeaders(origin, response.Headers())

            return nil
        },
        ResponseListenerPriority,
    )
}
