package http

import (
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestServerSentEventHub_BroadcastDeliversToTopicSubscribers(t *testing.T) {
    hub := NewServerSentEventHub()

    subscriber := hub.Subscribe("demo", 4)
    other := hub.Subscribe("other", 4)

    delivered := hub.Broadcast("demo", ServerSentEvent{Event: "ping", Data: "hello"})
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

func TestServerSentEventHub_BroadcastCountsDroppedEventsOnFullBuffer(t *testing.T) {
    hub := NewServerSentEventHub()

    hub.Subscribe("demo", 1)

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "first"}); 1 != delivered {
        t.Fatalf("expected the first event to be delivered, got %d", delivered)
    }

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "second"}); 0 != delivered {
        t.Fatalf("expected the second event to be dropped, got %d delivered", delivered)
    }

    if dropped := hub.DroppedEventCount(); 1 != dropped {
        t.Fatalf("expected exactly one dropped event, got %d", dropped)
    }
}

func TestServerSentEventHub_ShutdownClosesSubscribersAndStopsDelivery(t *testing.T) {
    hub := NewServerSentEventHub()

    first := hub.Subscribe("demo", 4)
    second := hub.Subscribe("other", 4)

    hub.Shutdown()

    for label, subscriber := range map[string]*ServerSentEventSubscriber{"demo": first, "other": second} {
        select {
        case _, open := <-subscriber.Events():
            if true == open {
                t.Fatalf("expected the %s subscriber channel to be closed", label)
            }
        default:
            t.Fatalf("expected a closed (non-blocking) read on the %s subscriber", label)
        }
    }

    if delivered := hub.Broadcast("demo", ServerSentEvent{Data: "x"}); 0 != delivered {
        t.Fatalf("expected no deliveries after shutdown, got %d", delivered)
    }

    hub.Shutdown()
}

func TestServerSentEventHub_SubscribeAfterShutdownReturnsClosedChannel(t *testing.T) {
    hub := NewServerSentEventHub()
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

func TestServerSentEventHub_UnsubscribeStopsDelivery(t *testing.T) {
    hub := NewServerSentEventHub()

    subscriber := hub.Subscribe("demo", 4)
    hub.Unsubscribe(subscriber)

    delivered := hub.Broadcast("demo", ServerSentEvent{Data: "x"})
    if 0 != delivered {
        t.Fatalf("expected 0 deliveries after unsubscribe, got %d", delivered)
    }

    if 0 != hub.SubscriberCount("demo") {
        t.Fatalf("expected no subscribers after unsubscribe")
    }
}

/** @info backplane */

type recordingBackplane struct {
    mutex     sync.Mutex
    published []ServerSentEvent
    publishErr error
}

func (instance *recordingBackplane) Publish(topic string, event ServerSentEvent) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil != instance.publishErr {
        return instance.publishErr
    }

    instance.published = append(instance.published, event)

    return nil
}

func (instance *recordingBackplane) Close() error {
    return nil
}

func (instance *recordingBackplane) count() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.published)
}

func TestServerSentEventHub_BroadcastReplicatesAndDeliversLocally(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", ServerSentEvent{Data: "hello"}); 1 != delivered {
        t.Fatalf("expected one local delivery, got %d", delivered)
    }

    if 1 != backplane.count() {
        t.Fatalf("expected the broadcast to be replicated once, got %d", backplane.count())
    }

    select {
    case event := <-subscriber.Events():
        if "hello" != event.Data {
            t.Fatalf("unexpected event delivered locally: %q", event.Data)
        }
    default:
        t.Fatalf("expected the event to be delivered to the local subscriber")
    }
}

func TestServerSentEventHub_DeliverLocalDoesNotReplicate(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    hub.DeliverLocal("orders", ServerSentEvent{Data: "remote"})

    if 0 != backplane.count() {
        t.Fatalf("expected DeliverLocal not to replicate, got %d", backplane.count())
    }

    select {
    case event := <-subscriber.Events():
        if "remote" != event.Data {
            t.Fatalf("unexpected event: %q", event.Data)
        }
    default:
        t.Fatalf("expected the remote event to reach the local subscriber")
    }
}

func TestServerSentEventHub_BroadcastAfterShutdownDoesNotReplicate(t *testing.T) {
    hub := NewServerSentEventHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    if delivered := hub.Broadcast("orders", ServerSentEvent{Data: "hello"}); 0 != delivered {
        t.Fatalf("expected no local delivery after shutdown, got %d", delivered)
    }

    if 0 != backplane.count() {
        t.Fatalf("expected no replication after shutdown, got %d", backplane.count())
    }
}

func TestServerSentEventHub_BackplaneFailureIsCounted(t *testing.T) {
    hub := NewServerSentEventHub()
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("backplane down", nil, nil)})

    hub.Broadcast("orders", ServerSentEvent{Data: "hello"})

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected one backplane failure, got %d", hub.BackplaneFailures())
    }
}
