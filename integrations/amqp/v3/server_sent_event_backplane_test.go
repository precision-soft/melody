package amqp_test

import (
    "errors"
    "os"
    "testing"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestServerSentEventBackplane_PublishAfterCloseDoesNotRetry(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := amqp.NewServerSentEventBackplane(amqp.ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            return nil, errors.New("no broker")
        },
        Hub: hub,
    })

    if closeErr := backplane.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    done := make(chan error, 1)
    go func() {
        done <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "after-close"})
    }()

    select {
    case publishErr := <-done:
        if nil == publishErr {
            t.Fatalf("expected publish on a closed backplane to fail")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("publish on a closed backplane hung instead of short-circuiting the retry")
    }
}

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp sse backplane integration test")
    }

    provider := amqp.NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    exchange := "melody.sse.test"

    hubA := melodyhttp.NewServerSentEventHub()
    backplaneA := amqp.NewServerSentEventBackplane(amqp.ServerSentEventBackplaneConfig{Connection: connection, Hub: hubA, Exchange: exchange})
    defer backplaneA.Close()

    hubB := melodyhttp.NewServerSentEventHub()
    backplaneB := amqp.NewServerSentEventBackplane(amqp.ServerSentEventBackplaneConfig{Connection: connection, Hub: hubB, Exchange: exchange})
    defer backplaneB.Close()

    subscriber := hubB.Subscribe("orders", 4)
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(10 * time.Second)
    tick := time.NewTicker(150 * time.Millisecond)
    defer tick.Stop()

    for {
        hubA.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "from-a"})

        select {
        case event := <-subscriber.Events():
            if "from-a" != event.Data {
                t.Fatalf("unexpected replicated event: %q", event.Data)
            }

            return
        case <-tick.C:
        case <-deadline:
            t.Fatalf("expected the broadcast to be replicated to the other instance")
        }
    }
}

func TestServerSentEventBackplane_DoesNotEchoToOriginInstanceTwice(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp sse backplane integration test")
    }

    provider := amqp.NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := amqp.NewServerSentEventBackplane(amqp.ServerSentEventBackplaneConfig{Connection: connection, Hub: hub, Exchange: "melody.sse.test.echo"})
    defer backplane.Close()

    subscriber := hub.Subscribe("orders", 4)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "once"}); 1 != delivered {
        t.Fatalf("expected exactly one local delivery, got %d", delivered)
    }

    select {
    case event := <-subscriber.Events():
        if "once" != event.Data {
            t.Fatalf("unexpected event: %q", event.Data)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the local delivery")
    }

    select {
    case event := <-subscriber.Events():
        t.Fatalf("expected no echoed re-delivery of the origin's own broadcast, got %q", event.Data)
    case <-time.After(time.Second):
    }
}
