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

    /* the boot-time dial stays as fail-fast VALIDATION of the DSN, but only as a probe that is closed at once: a connection opened here and handed to the transport would be owned by nobody — the transport closes only a connection it dialed itself — so it outlived every teardown. With just the dialer, the first use dials a connection the transport owns and its registered closer actually closes. */
    probe, openErr := provider.Open(dsn)
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }
    _ = probe.Close()

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.WelcomeEmail](registry, messageBusWelcomeType)

    return amqp.NewTransport(amqp.TransportConfig{
        Dialer:     provider.Dialer(dsn),
        Queue:      messageBusQueue,
        Prefetch:   10,
        Registry:   registry,
        DeadLetter: true,
    })
}
