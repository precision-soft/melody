package config

import (
    "reflect"

    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/messagehandler"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const messageBusTransportAsync = "async"

func (instance *Module) buildMessageBus() {
    transport := melodymessagebus.NewInMemoryTransport(64)

    locator := melodymessagebus.NewHandlerLocator()
    melodymessagebus.RegisterHandler(locator, messagehandler.HandleWelcomeEmail)
    melodymessagebus.RegisterHandler(locator, func(runtimeInstance melodyruntimecontract.Runtime, notification message.Notification) error {
        instance.sseHub.Broadcast(notification.Topic, melodyhttp.SseEvent{
            Event: "notification",
            Data:  notification.Text,
        })

        return nil
    })

    routing := map[reflect.Type]melodymessagebus.TransportRouting{
        reflect.TypeOf(message.WelcomeEmail{}): {
            Name:      messageBusTransportAsync,
            Transport: transport,
        },
    }

    instance.messageBusTransport = transport
    instance.messageBusDispatch = melodymessagebus.NewManager(
        "default",
        melodymessagebus.NewSendMessageMiddleware(routing),
        melodymessagebus.NewHandleMessageMiddleware(locator),
    )
    instance.messageBusConsume = melodymessagebus.NewManager(
        "default.consume",
        melodymessagebus.NewHandleMessageMiddleware(locator),
    )
    /** Messages whose handler keeps failing are retried with backoff and, once the attempts are
    exhausted, routed to a dead-letter transport for later inspection instead of being dropped. */
    deadLetterTransport := melodymessagebus.NewInMemoryTransport(64)
    instance.messageBusConsumeCommand = melodymessagebus.NewConsumeCommandWithRetry(
        instance.messageBusConsume,
        map[string]melodymessagebuscontract.Transport{
            messageBusTransportAsync: transport,
        },
        melodymessagebus.RetryPolicy{
            MaxRetries:       3,
            FailureTransport: deadLetterTransport,
        },
    )
}
