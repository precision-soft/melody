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

func TestSseHub_BroadcastCountsDroppedEventsOnFullBuffer(t *testing.T) {
    hub := http.NewSseHub()

    hub.Subscribe("demo", 1)

    if delivered := hub.Broadcast("demo", http.SseEvent{Data: "first"}); 1 != delivered {
        t.Fatalf("expected the first event to be delivered, got %d", delivered)
    }

    if delivered := hub.Broadcast("demo", http.SseEvent{Data: "second"}); 0 != delivered {
        t.Fatalf("expected the second event to be dropped, got %d delivered", delivered)
    }

    if dropped := hub.DroppedEventCount(); 1 != dropped {
        t.Fatalf("expected exactly one dropped event, got %d", dropped)
    }
}

func TestSseHub_ShutdownClosesSubscribersAndStopsDelivery(t *testing.T) {
    hub := http.NewSseHub()

    first := hub.Subscribe("demo", 4)
    second := hub.Subscribe("other", 4)

    hub.Shutdown()

    for label, subscriber := range map[string]*http.SseSubscriber{"demo": first, "other": second} {
        select {
        case _, open := <-subscriber.Events():
            if true == open {
                t.Fatalf("expected the %s subscriber channel to be closed", label)
            }
        default:
            t.Fatalf("expected a closed (non-blocking) read on the %s subscriber", label)
        }
    }

    if delivered := hub.Broadcast("demo", http.SseEvent{Data: "x"}); 0 != delivered {
        t.Fatalf("expected no deliveries after shutdown, got %d", delivered)
    }

    hub.Shutdown()
}

func TestSseHub_SubscribeAfterShutdownReturnsClosedChannel(t *testing.T) {
    hub := http.NewSseHub()
    hub.Shutdown()

    subscriber := hub.Subscribe("demo", 4)

    select {
    case _, open := <-subscriber.Events():
        if true == open {
            t.Fatalf("expected a post-shutdown subscriber to receive a closed channel")
        }
    default:
        t.Fatalf("expected a closed (non-blocking) read on a post-shutdown subscriber")
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
