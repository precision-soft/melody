package amqp

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const ServiceConnection = "service.amqp.connection"

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* RegisterConnectionService registers the connection under ServiceConnection with a closer, so the container's teardown closes it as Provider.CloseWithContext does, under the teardown's budget and the default close timeout, instead of the client's plain Close, which never returns over a write the peer stopped reading. */
func RegisterConnectionService(registrar ServiceRegistrar, connection *amqp091.Connection) {
    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*amqp091.Connection, error) {
            return connection, nil
        },
        container.RegisterOptions{Closer: container.NewCloser(func(closeContext context.Context, value *amqp091.Connection) error {
            if nil == value {
                return nil
            }

            closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, closeJoinTimeout), closeJoinTimeout, value)

            return closeErr
        })},
    )
}

func ConnectionMustFromResolver(resolver containercontract.Resolver) *amqp091.Connection {
    return container.MustFromResolver[*amqp091.Connection](resolver, ServiceConnection)
}

func ConnectionMustFromContainer(serviceContainer containercontract.Container) *amqp091.Connection {
    return container.MustFromResolver[*amqp091.Connection](serviceContainer, ServiceConnection)
}

func RegisterTransportService(registrar ServiceRegistrar, serviceName string, transport *Transport) {
    registrar.RegisterService(
        serviceName,
        func(resolver containercontract.Resolver) (messagebuscontract.Transport, error) {
            return transport, nil
        },
        container.RegisterOptions{},
    )
}

func TransportMustFromResolver(resolver containercontract.Resolver, serviceName string) messagebuscontract.Transport {
    return container.MustFromResolver[messagebuscontract.Transport](resolver, serviceName)
}
