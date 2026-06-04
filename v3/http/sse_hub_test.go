package http_test

import (
    "testing"

    "github.com/precision-soft/melody/v3/http"
)

func TestSseHub_BroadcastDeliversToTopicSubscribers(t *testing.T) {
    hub := http.NewSseHub()

    subscriber := hub.Subscribe("demo", 4)
    other := hub.Subscribe("other", 4)

    delivered := hub.Broadcast("demo", http.SseEvent{Event: "ping", Data: "hello"})
    if 1 != delivered {
        t.Fatalf("expected 1 delivery, got %d", delivered)
    }

    select {
    case event := <-subscriber.Events():
        if "ping" != event.Event || "hello" != event.Data {
            t.Fatalf("unexpected event: %+v", event)
        }
    default:
        t.Fatalf("expected an event on the demo subscriber")
    }

    select {
    case <-other.Events():
        t.Fatalf("did not expect an event on the other topic")
    default:
    }
}

func TestSseHub_UnsubscribeStopsDelivery(t *testing.T) {
    hub := http.NewSseHub()

    subscriber := hub.Subscribe("demo", 4)
    hub.Unsubscribe(subscriber)

    delivered := hub.Broadcast("demo", http.SseEvent{Data: "x"})
    if 0 != delivered {
        t.Fatalf("expected 0 deliveries after unsubscribe, got %d", delivered)
    }

    if 0 != hub.SubscriberCount("demo") {
        t.Fatalf("expected no subscribers after unsubscribe")
    }
}
