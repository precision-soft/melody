package amqp

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const ServiceConnection = "service.amqp.connection"

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* ConnectionServiceConfig is what a registered connection declares beyond the connection itself. */
type ConnectionServiceConfig struct {
    /* CloseTimeout bounds the stretches of the close that cannot observe the teardown's signal, as ProviderConfig.CloseTimeout does: 30s by default, at least 1ms, unlimited refused. */
    CloseTimeout bound.Duration
}

var connectionServiceCloseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ConnectionServiceConfig.CloseTimeout",
    Default: closeJoinTimeout,
    Minimum: bound.TimeoutMinimum,
}

/* RegisterConnectionService registers the connection under ServiceConnection with a closer, so the container's teardown closes it as Provider.CloseWithContext does, under the teardown's budget and the default close timeout, instead of the client's plain Close, which never returns over a write the peer stopped reading. The connection is live when it is filed and its own goroutines write it, so the teardown's walk does not enter it. */
func RegisterConnectionService(registrar ServiceRegistrar, connection *amqp091.Connection, config ConnectionServiceConfig) {
    closeTimeout, closeTimeoutErr := bound.Resolve(config.CloseTimeout, connectionServiceCloseTimeoutRule)
    if nil != closeTimeoutErr {
        exception.Panic(exception.FromError(closeTimeoutErr))
    }

    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*amqp091.Connection, error) {
            return connection, nil
        },
        container.RegisterOptions{TeardownReflectionDisabled: true, Closer: container.NewCloser(func(closeContext context.Context, value *amqp091.Connection) error {
            if nil == value {
                return nil
            }

            closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, closeTimeout.Value), closeTimeout.Value, value)

            return closeErr
        })},
    )
}

func ConnectionMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *amqp091.Connection {
    return container.MustFromResolver[*amqp091.Connection](ctx, resolver, ServiceConnection)
}

func ConnectionMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) *amqp091.Connection {
    return container.MustFromResolver[*amqp091.Connection](ctx, serviceContainer, ServiceConnection)
}

/* RegisterTransportService registers a pre-built transport under the name given. A transport may already be publishing or redialing when it is filed, rewriting its connection and channels under its own mutex, so the teardown's walk does not enter it; it is closed before ServiceConnection when that is registered, as an optional teardown dependency. */
func RegisterTransportService(registrar ServiceRegistrar, serviceName string, transport *Transport) {
    registrar.RegisterService(
        serviceName,
        func(resolver containercontract.Resolver) (messagebuscontract.Transport, error) {
            return transport, nil
        },
        container.RegisterOptions{TeardownReflectionDisabled: true, OptionalTeardownDependencyNames: []string{ServiceConnection}},
    )
}

func TransportMustFromResolver(ctx context.Context, resolver containercontract.Resolver, serviceName string) messagebuscontract.Transport {
    return container.MustFromResolver[messagebuscontract.Transport](ctx, resolver, serviceName)
}
