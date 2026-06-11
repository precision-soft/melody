package rueidis

import (
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    client := newTokenStoreClient(t)

    channel := WithServerSentEventBackplaneChannel("melody:sse:test:replicate")

    hubA := melodyhttp.NewServerSentEventHub()
    backplaneA := NewServerSentEventBackplane(client, hubA, channel)
    defer backplaneA.Close()

    hubB := melodyhttp.NewServerSentEventHub()
    backplaneB := NewServerSentEventBackplane(client, hubB, channel)
    defer backplaneB.Close()

    subscriber := hubB.Subscribe("orders", 4)
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(5 * time.Second)
    tick := time.NewTicker(100 * time.Millisecond)
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
    client := newTokenStoreClient(t)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(client, hub, WithServerSentEventBackplaneChannel("melody:sse:test:echo"))
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
    case <-time.After(500 * time.Millisecond):
    }
}

/** @info shouldResetBackplaneBackoff */

func TestShouldResetBackplaneBackoff(t *testing.T) {
    if true == shouldResetBackplaneBackoff(10*time.Microsecond) {
        t.Fatalf("a sub-second subscription (such as an immediate nil Receive return) must NOT reset backoff, otherwise listen() busy-loops re-subscribing with zero delay")
    }

    if true == shouldResetBackplaneBackoff(serverSentEventBackplaneInitialBackoff-time.Millisecond) {
        t.Fatalf("a subscription shorter than the healthy threshold must not reset backoff")
    }

    if false == shouldResetBackplaneBackoff(5*time.Second) {
        t.Fatalf("a healthy long-lived subscription must reset backoff for a fast reconnect")
    }
}
