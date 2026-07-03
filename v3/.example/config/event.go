package config

import (
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    eventDispatcher := kernelInstance.EventDispatcher()

    instance.registerRequiredRequestContextListener(eventDispatcher)

    eventDispatcher.AddSubscriber(
        subscriber.NewProductEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCategoryEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCurrencyEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewUserEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewSecurityAuthenticationEventSubscriber(),
    )
}

/* registerRequiredRequestContextListener demonstrates a required kernel.request listener. It prepares a per-request attribute that later stages depend on, so it must always run; marking it required through the event RequiredListenerRegistrar makes the kernel fail closed if any other kernel.request listener stops propagation before it — the same guarantee the security access-control listener gets automatically. A listener that deliberately short-circuits the request phase past required listeners would instead opt out with eventDispatcher.MarkListenerMaySkipRequiredListeners(registration). */
func (instance *Module) registerRequiredRequestContextListener(eventDispatcher melodyeventcontract.EventDispatcher) {
    registration := eventDispatcher.AddListener(
        melodykernelcontract.EventKernelRequest,
        func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
            requestEvent, ok := eventValue.Payload().(*melodyhttp.KernelRequestEvent)
            if false == ok || nil == requestEvent || nil == requestEvent.Request() {
                return nil
            }

            requestEvent.Request().Attributes().Set("example.requestContextReady", true)

            return nil
        },
        10,
    )

    if registrar, ok := eventDispatcher.(melodyeventcontract.RequiredListenerRegistrar); true == ok {
        registrar.MarkListenerRequired(registration)
    }
}

var _ melodyapplicationcontract.EventModule = (*Module)(nil)
