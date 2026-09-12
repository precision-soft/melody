package config

import (
    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/messagehandler"
    "github.com/precision-soft/melody/v3/.example/subscriber"
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
        /* resolved rather than captured, for the reason written at CatalogNotificationHubFromRuntime: a consumer that holds the hub the composition root built leaves the provider — and with it the logger swap and the container's teardown edge — unrun. */
        hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
        if nil != hubErr {
            return hubErr
        }

        hub.Broadcast(notification.Topic, melodyhttp.ServerSentEvent{
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

    /* the transport is handed ONLY a dialer, and nothing dials at boot. The rule is the one its twin states at buildOutboxTransport: a connection opened in the composition root is owned by nobody, because the transport closes only a connection it dialed itself, so the first use dials one the transport owns and its registered closer actually closes.

       The boot-time dial that used to stand here was fail-fast validation of the dsn, and it was paid by every process — a full amqp handshake before db:migrate --help prints its usage — while the transport dialed a second time at first use anyway. Its close was the plain Close of the client, which is an RPC over the send locks: on a broker that stopped reading, boot would have joined a write in flight and never returned. A dsn this application cannot dial now surfaces at the first publish, through the transport's own retry loop, which is where a broker that is merely down surfaces too. */
    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.WelcomeEmail](registry, messageBusWelcomeType)

    return amqp.NewTransport(amqp.TransportConfig{
        Dialer:     amqp.NewProvider().Dialer(dsn),
        Queue:      messageBusQueue,
        Prefetch:   10,
        Registry:   registry,
        DeadLetter: true,
    })
}
