package http

import (
	"net/http"

	eventcontract "github.com/precision-soft/melody/event/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
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

			if nil == responseEvent.Response() {
				responseEvent.SetResponse(EmptyResponse(http.StatusNoContent))
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
