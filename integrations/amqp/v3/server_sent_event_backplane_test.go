package amqp

import (
    "errors"
    "os"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestServerSentEventBackplane_PublishAfterCloseDoesNotRetry(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
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

func TestServerSentEventBackplane_CloseDoesNotDeadlockDuringReconnect(t *testing.T) {
    dialStarted := make(chan struct{}, 1)
    dialUnblock := make(chan struct{})

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            select {
            case dialStarted <- struct{}{}:
            default:
            }
            <-dialUnblock
            return nil, errors.New("dial cancelled")
        },
        Hub: hub,
    })

    select {
    case <-dialStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("dial never started")
    }

    done := make(chan error, 1)
    go func() { done <- backplane.Close() }()

    time.Sleep(50 * time.Millisecond)
    close(dialUnblock)

    select {
    case closeErr := <-done:
        if nil != closeErr {
            t.Fatalf("close: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close() deadlocked — mutex was held during dial and blocked Close()")
    }
}

func TestServerSentEventBackplane_CloseReturnsWhileDialStillBlocked(t *testing.T) {
    dialStarted := make(chan struct{}, 1)
    dialUnblock := make(chan struct{})

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            select {
            case dialStarted <- struct{}{}:
            default:
            }
            <-dialUnblock
            return nil, errors.New("dial released")
        },
        Hub: hub,
    })

    select {
    case <-dialStarted:
    case <-time.After(2 * time.Second):
        close(dialUnblock)
        t.Fatalf("dial never started")
    }

    done := make(chan error, 1)
    go func() { done <- backplane.Close() }()

    select {
    case closeErr := <-done:
        if nil != closeErr {
            close(dialUnblock)
            t.Fatalf("close: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        close(dialUnblock)
        t.Fatalf("Close() blocked on the in-flight dial instead of returning once the context was cancelled")
    }

    close(dialUnblock)
}

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp sse backplane integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    exchange := "melody.sse.test"

    hubA := melodyhttp.NewServerSentEventHub()
    backplaneA := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hubA, Exchange: exchange})
    defer backplaneA.Close()

    hubB := melodyhttp.NewServerSentEventHub()
    backplaneB := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hubB, Exchange: exchange})
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

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hub, Exchange: "melody.sse.test.echo"})
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

/** @info reconnect backoff reset */

func TestShouldResetReconnectBackoff(t *testing.T) {
    if true == shouldResetReconnectBackoff(reconnectInitialBackoff-time.Nanosecond) {
        t.Fatalf("expected no backoff reset for a subscription that died sooner than the initial backoff")
    }

    if false == shouldResetReconnectBackoff(reconnectInitialBackoff) {
        t.Fatalf("expected a backoff reset for a subscription that lived at least the initial backoff")
    }

    if false == shouldResetReconnectBackoff(2*reconnectInitialBackoff) {
        t.Fatalf("expected a backoff reset for a long-lived subscription")
    }
}

/** @info publish channel reopen */

func TestServerSentEventBackplane_EnsurePublishChannel_ReopensClosedChannel(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection: connection,
        Hub:        melodyhttp.NewServerSentEventHub(),
        Exchange:   "melody.sse.reopen-publish",
    })

    first, firstErr := backplane.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := backplane.ensurePublishChannel()
    if nil != secondErr {
        t.Fatalf("second ensurePublishChannel: %v", secondErr)
    }
    if true == second.IsClosed() {
        t.Fatalf("expected a fresh open channel, got a closed one (the stale channel was reused)")
    }
    if second == first {
        t.Fatalf("expected the stale closed channel to be replaced, got the same channel")
    }
}
