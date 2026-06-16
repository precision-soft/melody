package messagebus

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const (
    ServiceBus            = "service.messagebus.bus"
    ServiceHandlerLocator = "service.messagebus.handler_locator"
)

func BusMustFromContainer(serviceContainer containercontract.Container) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](serviceContainer, ServiceBus)
}

func BusMustFromResolver(resolver containercontract.Resolver) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](resolver, ServiceBus)
}

func HandlerLocatorMustFromContainer(serviceContainer containercontract.Container) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](serviceContainer, ServiceHandlerLocator)
}

func HandlerLocatorMustFromResolver(resolver containercontract.Resolver) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](resolver, ServiceHandlerLocator)
}
