package http

import (
    "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

const (
    KernelResponseNormalizerListenerPriority = 100
)

func RegisterKernelResponseNormalizerListener(eventDispatcher eventcontract.EventDispatcher) {
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

            /* the kernel replaces a handler's nil with an empty 204 before dispatching, so nil here is a listener that cleared it, which writeResponse answers the same way */
            if nil == responseEvent.Response() {
                return nil
            }

            if 0 == responseEvent.Response().StatusCode() {
                responseEvent.Response().SetStatusCode(http.StatusOK)
            }

            if nil == responseEvent.Response().Headers() {
                responseEvent.Response().SetHeaders(make(http.Header))
            }

            return nil
        },
        eventcontract.ListenerConfig{Priority: KernelResponseNormalizerListenerPriority},
    )
}
