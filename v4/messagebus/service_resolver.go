package messagebus

import (
    "context"
    "errors"
    "fmt"
    "runtime/debug"
    "sort"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
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
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

func BusMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](ctx, serviceContainer, ServiceBus)
}

func BusMustFromResolver(ctx context.Context, resolver containercontract.Resolver) messagebuscontract.Bus {
    return container.MustFromResolver[messagebuscontract.Bus](ctx, resolver, ServiceBus)
}

/* ConsumeBusFromResolver returns the bus the consumer dispatches received messages into, preferring a dedicated consume bus and falling back to the shared bus so single-bus applications need no extra wiring. */
func ConsumeBusFromResolver(ctx context.Context, resolver containercontract.Resolver) messagebuscontract.Bus {
    if true == resolver.Has(ServiceConsumeBus) {
        return container.MustFromResolver[messagebuscontract.Bus](ctx, resolver, ServiceConsumeBus)
    }

    return BusMustFromResolver(ctx, resolver)
}

/* RegisterTransports registers the named transports the consume command resolves at run time, which is enough for the framework to expose melody:messagebus:consume. It also registers a TransportsCloser as a dependency of the map, since the ordered teardown closes what answers CloseWithContext and a map answers nothing, so the transports close after every consumer that depends on them. */
func RegisterTransports(registrar ServiceRegistrar, transports map[string]messagebuscontract.Transport) {
    /* a nil or typed-nil entry is refused at boot, as RouteType refuses one; the closer's own nil branch covers an entry written into the map after this call */
    for name, transport := range transports {
        if true == isNilTransport(transport) {
            exception.Panic(exception.NewError("messagebus transport is nil", map[string]any{"name": name}, nil))
        }
    }

    registrar.RegisterService(
        ServiceTransportsCloser,
        func(resolver containercontract.Resolver) (*TransportsCloser, error) {
            return &TransportsCloser{transports: transports}, nil
        },
        container.RegisterOptions{},
    )

    registrar.RegisterService(
        ServiceTransports,
        func(resolver containercontract.Resolver) (map[string]messagebuscontract.Transport, error) {
            /* resolving the closer through this provider records the dependency edge the teardown orders by: the map's consumers close first, the transports after them */
            container.MustFromResolver[*TransportsCloser](container.ContextOf(resolver), resolver, ServiceTransportsCloser)

            return transports, nil
        },
        container.RegisterOptions{},
    )
}

/* TransportsCloser owns the shutdown of every registered transport; the container's ordered teardown closes it once whoever resolved the transports map is already closed. */
type TransportsCloser struct {
    transports map[string]messagebuscontract.Transport
}

/* CloseWithContext closes every transport under the teardown's deadline. The transports close serially, in sorted name order, so they share the one deadline rather than each getting a copy, and the failure map names every one that failed. */
func (instance *TransportsCloser) CloseWithContext(closeContext context.Context) error {
    names := make([]string, 0, len(instance.transports))
    for name := range instance.transports {
        names = append(names, name)
    }
    /* sorted, so the transports close in the same order on every run */
    sort.Strings(names)

    var closeErrs []error
    for _, name := range names {
        /* a nil entry is skipped, since its panic would abandon this loop and leave every transport sorted after it unclosed; RegisterTransports refuses one at boot, and this branch answers for one written into the map later */
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

/* isNilTransport reads the entry through the typed-nil door, since a nil pointer inside a non-nil interface passes a plain comparison and then dereferences inside CloseWithContext. */
func isNilTransport(transport messagebuscontract.Transport) bool {
    return true == internal.IsNilInterface(transport)
}

/* closeOne contains a panicking transport close as a returned failure, so the transports sorted after it still close. The recovered value travels as the cause, and the stack is captured inside the recover, the only place its frames still exist. */
func (instance *TransportsCloser) closeOne(closeContext context.Context, name string) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "messagebus transport close panicked",
            map[string]any{
                "transport":     name,
                "recoveredType": fmt.Sprintf("%T", recoveredValue),
                "panicStack":    string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )
    }()

    transport := instance.transports[name]

    closeErr = transport.CloseWithContext(closeContext)

    /* a typed nil is the nil its producer meant, as the container's own close reads it */
    if true == internal.IsNilInterface(closeErr) {
        return nil
    }

    return closeErr
}

func TransportsMustFromResolver(ctx context.Context, resolver containercontract.Resolver) map[string]messagebuscontract.Transport {
    return container.MustFromResolver[map[string]messagebuscontract.Transport](ctx, resolver, ServiceTransports)
}

/* RetryPolicyFromResolver returns the application-provided retry policy when one is registered; the second result is false when the consumer should keep the framework defaults. */
func RetryPolicyFromResolver(ctx context.Context, resolver containercontract.Resolver) (RetryPolicy, bool) {
    if false == resolver.Has(ServiceRetryPolicy) {
        return RetryPolicy{}, false
    }

    return container.MustFromResolver[RetryPolicy](ctx, resolver, ServiceRetryPolicy), true
}

func HandlerLocatorMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](ctx, serviceContainer, ServiceHandlerLocator)
}

func HandlerLocatorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) messagebuscontract.HandlerLocator {
    return container.MustFromResolver[messagebuscontract.HandlerLocator](ctx, resolver, ServiceHandlerLocator)
}
