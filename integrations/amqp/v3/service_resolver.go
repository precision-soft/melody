package amqp

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

const ServiceConnection = "service.amqp.connection"

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterConnectionService(registrar ServiceRegistrar, connection *amqp091.Connection) {
    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*amqp091.Connection, error) {
            return connection, nil
        },
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
    )
}

func TransportMustFromResolver(resolver containercontract.Resolver, serviceName string) messagebuscontract.Transport {
    return container.MustFromResolver[messagebuscontract.Transport](resolver, serviceName)
}
