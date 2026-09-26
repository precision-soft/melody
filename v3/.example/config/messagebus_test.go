package config

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

/* Nothing dials at boot. The dsn below points at an address no broker answers on, and the transport is built over it without a packet leaving the process; a dsn this application cannot dial surfaces at the first publish, through the transport's own retry loop. The test is written as a deadline rather than as an assertion on the return, because the failure it guards against is a dial: the address is a discard port on a host that does not resolve, so a boot-time dial would spend the resolver's own timeout before it panicked. */
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

/* Without a dsn the example runs the whole message bus in process, which is what makes a checkout without a broker functional rather than merely bootable. */
func TestBuildMessageBusTransport_FallsBackToTheInProcessTransportWithoutADsn(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    transport := moduleInstance.buildMessageBusTransport()

    if _, isInMemory := transport.(*melodymessagebus.InMemoryTransport); false == isInMemory {
        t.Fatalf("expected the in-process transport without a dsn, got %T", transport)
    }
}

/* the notification handler resolves the hub through the runtime at each message: the hub the container publishes is the one that broadcasts, not the one the composition root held when the bus was built, and a capture would leave the hub's provider, its logger swap and its teardown edge unrun. The module's own hub and the registered one are two hubs here, so a capture broadcasts where nobody listens. */
func TestBuildMessageBus_TheNotificationHandlerResolvesTheHubAtEachMessage(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildServerSentEvent()
    moduleInstance.buildMessageBus()

    registeredHub := melodyhttp.NewServerSentEventHub()
    t.Cleanup(registeredHub.Shutdown)

    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return registeredHub, nil
        },
    )

    listener := registeredHub.Subscribe("catalog", 1)
    capturedListener := moduleInstance.serverSentEventHub.Subscribe("catalog", 1)

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    if _, dispatchErr := moduleInstance.messageBusDispatch.Dispatch(runtimeInstance, message.Notification{Topic: "catalog", Text: "changed"}); nil != dispatchErr {
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

/* the dispatch bus claims the Bus contract on the type index and the consume bus is asked for by name only:
   both are the same contract, and a second registration by type is refused by the container */
func TestRegisterMessageBusServices_TheDispatchBusOwnsTheTypeAndTheConsumeBusIsByName(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildServerSentEvent()
    moduleInstance.buildMessageBus()

    containerInstance := melodycontainer.NewContainer()
    moduleInstance.registerMessageBusServices(containerRegistrar{Container: containerInstance})

    byType, byTypeErr := melodycontainer.FromResolverByType[melodymessagebuscontract.Bus](containerInstance)
    if nil != byTypeErr || moduleInstance.messageBusDispatch != byType {
        t.Fatalf("expected the dispatch bus on the type index, got %v, %v", byType, byTypeErr)
    }

    consume, consumeErr := melodycontainer.FromResolver[melodymessagebuscontract.Bus](containerInstance, melodymessagebus.ServiceConsumeBus)
    if nil != consumeErr || moduleInstance.messageBusConsume != consume {
        t.Fatalf("expected the consume bus by name, got %v, %v", consume, consumeErr)
    }
}
