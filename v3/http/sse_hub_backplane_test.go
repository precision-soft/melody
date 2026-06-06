package http_test

import (
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
)

type recordingBackplane struct {
    mutex     sync.Mutex
    published []http.SseEvent
    publishErr error
}

func (instance *recordingBackplane) Publish(topic string, event http.SseEvent) error {
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

func TestSseHub_BroadcastReplicatesAndDeliversLocally(t *testing.T) {
    hub := http.NewSseHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", http.SseEvent{Data: "hello"}); 1 != delivered {
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

func TestSseHub_DeliverLocalDoesNotReplicate(t *testing.T) {
    hub := http.NewSseHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    subscriber := hub.Subscribe("orders", 1)
    defer hub.Unsubscribe(subscriber)

    hub.DeliverLocal("orders", http.SseEvent{Data: "remote"})

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

func TestSseHub_BroadcastAfterShutdownDoesNotReplicate(t *testing.T) {
    hub := http.NewSseHub()
    backplane := &recordingBackplane{}
    hub.SetBackplane(backplane)

    hub.Shutdown()

    if delivered := hub.Broadcast("orders", http.SseEvent{Data: "hello"}); 0 != delivered {
        t.Fatalf("expected no local delivery after shutdown, got %d", delivered)
    }

    if 0 != backplane.count() {
        t.Fatalf("expected no replication after shutdown, got %d", backplane.count())
    }
}

func TestSseHub_BackplaneFailureIsCounted(t *testing.T) {
    hub := http.NewSseHub()
    hub.SetBackplane(&recordingBackplane{publishErr: exception.NewError("backplane down", nil, nil)})

    hub.Broadcast("orders", http.SseEvent{Data: "hello"})

    if 1 != hub.BackplaneFailures() {
        t.Fatalf("expected one backplane failure, got %d", hub.BackplaneFailures())
    }
}
