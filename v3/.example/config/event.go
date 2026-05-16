package config

import (
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    eventDispatcher := kernelInstance.EventDispatcher()

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

var _ melodyapplicationcontract.EventModule = (*Module)(nil)
