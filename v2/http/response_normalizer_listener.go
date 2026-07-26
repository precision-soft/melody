package http

import (
    "net/http"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const (
    KernelResponseNormalizerListenerPriority = 100
)

func RegisterKernelResponseNormalizerListener(eventDispatcher eventcontract.EventDispatcher) {
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

            /* the kernel replaces a handler's nil with an empty 204 before it dispatches the event, so the normalizer is never the one to invent a response; nil here can only be a listener that ran earlier and cleared it, which writeResponse answers with the same empty 204. Synthesizing it a second time here would be a second place deciding what "no response" means. */
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
        KernelResponseNormalizerListenerPriority,
    )
}
