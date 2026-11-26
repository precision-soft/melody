package amqp

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

const ServiceConnection = "service.amqp.connection"

type serviceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* registerConnectionService registers the factory of the connection owner, so the container's teardown closes it through its own CloseWithContext, under the teardown's budget and the close timeout the owner carries. The connection's own goroutines write it, so the teardown's walk does not enter it. */
func registerConnectionService(registrar serviceRegistrar, connection func(resolver containercontract.Resolver) (*Connection, error)) {
    registrar.RegisterService(
        ServiceConnection,
        connection,
        container.RegisterOptions{TeardownReflectionDisabled: true},
    )
}

/* ConnectionMustFromRuntime answers the connection service resolved through the runtime's scope; the container keeps its lifecycle. */
func ConnectionMustFromRuntime(runtimeInstance containercontract.Runtime) *Connection {
    return runtime.MustFromRuntime[*Connection](runtimeInstance, ServiceConnection)
}

func ConnectionMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Connection {
    return container.MustFromResolver[*Connection](ctx, resolver, ServiceConnection)
}

/* registerTransportService registers the provider of a transport under the name given, run at first resolution, so a transport over the shared connection resolves the connection service. A transport may be publishing or redialing once built, rewriting its connection and channels under its own mutex, so the teardown's walk does not enter it; it is closed before ServiceConnection when that is registered, as an optional teardown dependency. */
func registerTransportService(registrar serviceRegistrar, serviceName string, provider func(resolver containercontract.Resolver) (*Transport, error)) {
    registrar.RegisterService(
        serviceName,
        func(resolver containercontract.Resolver) (messagebuscontract.Transport, error) {
            transport, providerErr := provider(resolver)
            if nil != providerErr {
                return nil, providerErr
            }

            if nil == transport {
                return nil, exception.NewError("the amqp transport provider answered no transport and no error", map[string]any{"transport": serviceName}, nil)
            }

            return transport, nil
        },
        container.RegisterOptions{TeardownReflectionDisabled: true, OptionalTeardownDependencyNames: []string{ServiceConnection}},
    )
}

func TransportMustFromResolver(ctx context.Context, resolver containercontract.Resolver, serviceName string) messagebuscontract.Transport {
    return container.MustFromResolver[messagebuscontract.Transport](ctx, resolver, serviceName)
}
