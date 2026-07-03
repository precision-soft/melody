package messagebus

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const (
    ServiceBus            = "service.messagebus.bus"
    ServiceConsumeBus     = "service.messagebus.consume_bus"
    ServiceHandlerLocator = "service.messagebus.handler_locator"
    ServiceTransports     = "service.messagebus.transports"
    ServiceRetryPolicy    = "service.messagebus.retry_policy"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func BusMustFromContainer(serviceContainer containercontract.Container) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](serviceContainer, ServiceBus)
}

func BusMustFromResolver(resolver containercontract.Resolver) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](resolver, ServiceBus)
}

/* ConsumeBusFromResolver returns the bus the consumer dispatches received messages into, preferring a dedicated consume bus and falling back to the shared bus so single-bus applications need no extra wiring. */
func ConsumeBusFromResolver(resolver containercontract.Resolver) messagebuscontract.Bus {
    if true == resolver.Has(ServiceConsumeBus) {
        return container.MustFromResolver[messagebuscontract.Bus](resolver, ServiceConsumeBus)
    }

    return BusMustFromResolver(resolver)
}

/* RegisterTransports registers the named transports the consume command resolves at run time, so registering them is enough for the framework to expose melody:messagebus:consume. */
func RegisterTransports(registrar ServiceRegistrar, transports map[string]messagebuscontract.Transport) {
    registrar.RegisterService(
        ServiceTransports,
        func(resolver containercontract.Resolver) (map[string]messagebuscontract.Transport, error) {
            return transports, nil
        },
    )
}

func TransportsMustFromResolver(resolver containercontract.Resolver) map[string]messagebuscontract.Transport {
    return container.MustFromResolver[map[string]messagebuscontract.Transport](resolver, ServiceTransports)
}

/* RetryPolicyFromResolver returns the application-provided retry policy when one is registered; the second result is false when the consumer should keep the framework defaults. */
func RetryPolicyFromResolver(resolver containercontract.Resolver) (RetryPolicy, bool) {
    if false == resolver.Has(ServiceRetryPolicy) {
        return RetryPolicy{}, false
    }

    return container.MustFromResolver[RetryPolicy](resolver, ServiceRetryPolicy), true
}

func HandlerLocatorMustFromContainer(serviceContainer containercontract.Container) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](serviceContainer, ServiceHandlerLocator)
}

func HandlerLocatorMustFromResolver(resolver containercontract.Resolver) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](resolver, ServiceHandlerLocator)
}
