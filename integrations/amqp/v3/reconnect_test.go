package amqp

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func newReconnectRuntime(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

func TestNextBackoff_GrowsAndCaps(t *testing.T) {
    expected := []time.Duration{
        2 * time.Second,
        4 * time.Second,
        8 * time.Second,
        16 * time.Second,
        30 * time.Second,
        30 * time.Second,
    }

    current := reconnectInitialBackoff
    for index, want := range expected {
        current = nextBackoff(current)
        if want != current {
            t.Fatalf("step %d: expected %s, got %s", index, want, current)
        }
    }
}

func TestConnect_NoDialerReturnsError(t *testing.T) {
    instance := &Transport{queue: "orders"}

    _, connectErr := instance.connect()
    if nil == connectErr {
        t.Fatalf("expected an error when no connection and no dialer are configured")
    }
}

func TestConnect_DialFailureIsWrapped(t *testing.T) {
    calls := 0
    instance := &Transport{
        queue: "orders",
        dialer: func() (*amqp091.Connection, error) {
            calls++
            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    _, connectErr := instance.connect()
    if nil == connectErr {
        t.Fatalf("expected the dial failure to surface")
    }

    if 1 != calls {
        t.Fatalf("expected the dialer to be invoked once, got %d", calls)
    }

    if true == instance.reconnecting {
        t.Fatalf("expected the reconnecting flag to be cleared after a failed dial")
    }
}

func TestConnect_SingleFlight(t *testing.T) {
    entered := make(chan struct{})
    release := make(chan struct{})

    instance := &Transport{
        queue: "orders",
        dialer: func() (*amqp091.Connection, error) {
            close(entered)
            <-release
            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    go instance.connect()

    <-entered

    _, secondErr := instance.connect()
    if errReconnectInProgress != secondErr {
        t.Fatalf("expected a concurrent connect to report reconnect-in-progress, got %v", secondErr)
    }

    close(release)
}

func TestForwardDeliveries_ChannelLost(t *testing.T) {
    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders"}
    out := make(chan messagebuscontract.Envelope, 1)

    reason := instance.forwardDeliveries(newReconnectRuntime(context.Background()), nil, deliveries, out)
    if forwardChannelLost != reason {
        t.Fatalf("expected forwardChannelLost, got %v", reason)
    }
}

func TestForwardDeliveries_ContextDone(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    instance := &Transport{queue: "orders"}
    deliveries := make(chan amqp091.Delivery)
    out := make(chan messagebuscontract.Envelope, 1)

    reason := instance.forwardDeliveries(newReconnectRuntime(ctx), nil, deliveries, out)
    if forwardDone != reason {
        t.Fatalf("expected forwardDone, got %v", reason)
    }
}

func TestConsumeLoop_NoDialerClosesOut(t *testing.T) {
    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders"}
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(newReconnectRuntime(context.Background()), nil, deliveries, out)

    select {
    case _, open := <-out:
        if true == open {
            t.Fatalf("expected out to be closed without delivering a message")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected consumeLoop to close out after the channel was lost")
    }
}

func TestConsumeLoop_ContextDoneClosesOut(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    instance := &Transport{queue: "orders"}
    deliveries := make(chan amqp091.Delivery)
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(newReconnectRuntime(ctx), nil, deliveries, out)

    cancel()

    select {
    case _, open := <-out:
        if true == open {
            t.Fatalf("expected out to be closed on context cancellation")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected consumeLoop to close out after context cancellation")
    }
}
