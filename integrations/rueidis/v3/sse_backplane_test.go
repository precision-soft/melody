package rueidis_test

import (
    "testing"
    "time"

    rueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestSseBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    client := newTokenStoreClient(t)

    channel := rueidis.WithSseBackplaneChannel("melody:sse:test:replicate")

    hubA := melodyhttp.NewSseHub()
    backplaneA := rueidis.NewSseBackplane(client, hubA, channel)
    defer backplaneA.Close()

    hubB := melodyhttp.NewSseHub()
    backplaneB := rueidis.NewSseBackplane(client, hubB, channel)
    defer backplaneB.Close()

    subscriber := hubB.Subscribe("orders", 4)
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(5 * time.Second)
    tick := time.NewTicker(100 * time.Millisecond)
    defer tick.Stop()

    for {
        hubA.Broadcast("orders", melodyhttp.SseEvent{Data: "from-a"})

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

func TestSseBackplane_DoesNotEchoToOriginInstanceTwice(t *testing.T) {
    client := newTokenStoreClient(t)

    hub := melodyhttp.NewSseHub()
    backplane := rueidis.NewSseBackplane(client, hub, rueidis.WithSseBackplaneChannel("melody:sse:test:echo"))
    defer backplane.Close()

    subscriber := hub.Subscribe("orders", 4)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", melodyhttp.SseEvent{Data: "once"}); 1 != delivered {
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
