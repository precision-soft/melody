package config

import (
    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/messagehandler"
    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    messageBusTransportAsync = "async"
    messageBusQueue          = "welcome_email"
    messageBusWelcomeType    = "welcome_email"
)

func (instance *Module) buildMessageBus() {
    transport := instance.buildMessageBusTransport()

    locator := melodymessagebus.NewHandlerLocator()
    melodymessagebus.RegisterHandler(locator, messagehandler.HandleWelcomeEmail)
    melodymessagebus.RegisterHandler(locator, func(runtimeInstance melodyruntimecontract.Runtime, notification message.Notification) error {
        instance.serverSentEventHub.Broadcast(notification.Topic, melodyhttp.ServerSentEvent{
            Event: "notification",
            Data:  notification.Text,
        })

        return nil
    })

    routing := melodymessagebus.NewRouting()
    melodymessagebus.RouteType[message.WelcomeEmail](routing, messageBusTransportAsync, transport)

    instance.messageBusTransport = transport
    instance.messageBusDispatch = melodymessagebus.NewManager(
        "default",
        melodymessagebus.NewSendMessageMiddlewareFromRouting(routing),
        melodymessagebus.NewHandleMessageMiddleware(locator),
    )
    instance.messageBusConsume = melodymessagebus.NewManager(
        "default.consume",
        melodymessagebus.NewHandleMessageMiddleware(locator),
    )
}

func (instance *Module) buildMessageBusTransport() melodymessagebuscontract.Transport {
    dsn := instance.environmentValue(environmentKeyAmqpDsn)
    if "" == dsn {
        return melodymessagebus.NewInMemoryTransport(64)
    }

    provider := amqp.NewProvider()

    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.WelcomeEmail](registry, messageBusWelcomeType)

    return amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Dialer:     provider.Dialer(dsn),
        Queue:      messageBusQueue,
        Prefetch:   10,
        Registry:   registry,
        DeadLetter: true,
    })
}
