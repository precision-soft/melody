package messagebus

import (
    "context"
    "errors"
    "fmt"
    "sort"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const (
    ServiceBus              = "service.messagebus.bus"
    ServiceConsumeBus       = "service.messagebus.consume_bus"
    ServiceHandlerLocator   = "service.messagebus.handler_locator"
    ServiceTransports       = "service.messagebus.transports"
    ServiceTransportsCloser = "service.messagebus.transports_closer"
    ServiceRetryPolicy      = "service.messagebus.retry_policy"
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

/* RegisterTransports registers the named transports the consume command resolves at run time, so registering them is enough for the framework to expose melody:messagebus:consume. It also registers a TransportsCloser beside the map, resolved as a dependency of it: the container's ordered teardown closes what answers Close() error and a map answers nothing, so without the closer no transport could ever join the shutdown — whichever run resolves the transports thereby guarantees their broker connections are closed after every consumer that depends on them. */
func RegisterTransports(registrar ServiceRegistrar, transports map[string]messagebuscontract.Transport) {
    registrar.RegisterService(
        ServiceTransportsCloser,
        func(resolver containercontract.Resolver) (*TransportsCloser, error) {
            return &TransportsCloser{transports: transports}, nil
        },
    )

    registrar.RegisterService(
        ServiceTransports,
        func(resolver containercontract.Resolver) (map[string]messagebuscontract.Transport, error) {

            container.MustFromResolver[*TransportsCloser](resolver, ServiceTransportsCloser)

            return transports, nil
        },
    )
}

/* TransportsCloser owns the shutdown of every registered transport; the container's ordered teardown closes it once whoever resolved the transports map is already closed. */
type TransportsCloser struct {
    transports map[string]messagebuscontract.Transport
}

func (instance *TransportsCloser) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext closes transports serially in sorted name order, passing the shared caller deadline to context-aware transports. Earlier closes spend budget unavailable to later ones; failures identify each affected transport. */
func (instance *TransportsCloser) CloseWithContext(closeContext context.Context) error {
    names := make([]string, 0, len(instance.transports))
    for name := range instance.transports {
        names = append(names, name)
    }

    sort.Strings(names)

    var closeErrs []error
    for _, name := range names {

        if true == isNilTransport(instance.transports[name]) {
            closeErrs = append(
                closeErrs,
                exception.NewError("messagebus transport is nil and was not closed", map[string]any{"transport": name}, nil),
            )

            continue
        }

        if closeErr := instance.closeOne(closeContext, name); nil != closeErr {
            closeErrs = append(
                closeErrs,
                exception.NewError("messagebus transport close failed", map[string]any{"transport": name}, closeErr),
            )
        }
    }

    return errors.Join(closeErrs...)
}

func isNilTransport(transport messagebuscontract.Transport) bool {
    return true == internal.IsNilInterface(transport)
}

func (instance *TransportsCloser) closeOne(closeContext context.Context, name string) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "messagebus transport close panicked",
            map[string]any{"transport": name, "recoveredType": fmt.Sprintf("%T", recoveredValue)},
            exception.PanicCause(recoveredValue),
        )
    }()

    transport := instance.transports[name]

    contextCloseable, isContextCloseable := transport.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloseable {
        return contextCloseable.CloseWithContext(closeContext)
    }

    return transport.Close()
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
