package config

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/message"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* an address no broker answers on: the transport dials nothing at boot */
func TestBuildMessageBusTransport_DoesNotDialAtBoot(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        "AMQP_DSN": "amqp://guest:guest@broker.invalid:5672/",
    })

    built := make(chan any, 1)
    go func() {
        defer func() {
            built <- recover()
        }()

        transport := moduleInstance.buildMessageBusTransport()
        if nil == transport {
            t.Error("expected a transport built over the dsn")
        }
    }()

    select {
    case recovered := <-built:
        if nil != recovered {
            t.Fatalf("expected the transport to be built without dialing, it panicked: %v", recovered)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("building the transport did not return within 2s, so something dialed")
    }
}

func TestBuildMessageBusTransport_FallsBackToTheInProcessTransportWithoutADsn(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    transport := moduleInstance.buildMessageBusTransport()

    if _, isInMemory := transport.(*melodymessagebus.InMemoryTransport); false == isInMemory {
        t.Fatalf("expected the in-process transport without a dsn, got %T", transport)
    }
}

func TestBuildMessageBus_TheNotificationHandlerResolvesTheHubAtEachMessage(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildMessageBus()

    builtHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    t.Cleanup(func() { _ = builtHub.CloseWithContext(context.Background()) })
    moduleInstance.builtServerSentEventHub.Store(builtHub)

    registeredHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    t.Cleanup(func() { _ = registeredHub.CloseWithContext(context.Background()) })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return registeredHub, nil
        },
        melodycontainer.RegisterOptions{},
    )

    listener := subscribeTestTopic(t, registeredHub, "catalog")
    capturedListener := subscribeTestTopic(t, builtHub, "catalog")

    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
    if _, dispatchErr := moduleInstance.messageBusDispatch.Dispatch(runtimeInstance, message.Notification{Topic: "catalog", Text: "changed"}, melodymessagebuscontract.DispatchOptions{}); nil != dispatchErr {
        t.Fatalf("dispatch the notification: %v", dispatchErr)
    }

    select {
    case received := <-listener.Events():
        if "notification" != received.Event || "changed" != received.Data {
            t.Fatalf("expected the notification event, got %+v", received)
        }
    default:
        t.Fatal("expected the hub the container publishes to broadcast the notification")
    }

    select {
    case received := <-capturedListener.Events():
        t.Fatalf("expected the module's own hub to broadcast nothing, got %+v", received)
    default:
    }
}

func TestRegisterMessageBusServices_TheDispatchBusOwnsTheTypeAndTheConsumeBusIsByName(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildMessageBus()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    moduleInstance.registerMessageBusServices(containerRegistrar{Container: containerInstance})

    byType, byTypeErr := melodycontainer.FromResolverByType[melodymessagebuscontract.Bus](t.Context(), containerInstance)
    if nil != byTypeErr || moduleInstance.messageBusDispatch != byType {
        t.Fatalf("expected the dispatch bus on the type index, got %v, %v", byType, byTypeErr)
    }

    consume, consumeErr := melodycontainer.FromResolver[melodymessagebuscontract.Bus](t.Context(), containerInstance, melodymessagebus.ServiceConsumeBus)
    if nil != consumeErr || moduleInstance.messageBusConsume != consume {
        t.Fatalf("expected the consume bus by name, got %v, %v", consume, consumeErr)
    }
}
