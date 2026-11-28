package event

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

func newSubscriberProbeListener(recorder *string, name string) eventcontract.EventListener {
    return func(runtimeInstance containercontract.Runtime, eventInstance eventcontract.Event) error {
        *recorder = name

        return nil
    }
}

func TestNewSubscribedEvent_CarriesTheListenerAndItsPriority(t *testing.T) {
    ranListener := ""
    listener := newSubscriberProbeListener(&ranListener, "probe")

    subscribed := NewSubscribedEvent(listener, eventcontract.ListenerConfig{Priority: 128})

    /* a func value is not comparable, so the listener is identified by running it */
    if runErr := subscribed.Listener()(nil, nil); nil != runErr {
        t.Fatalf("unexpected listener error: %v", runErr)
    }

    if "probe" != ranListener {
        t.Fatalf("expected the subscription to carry the listener it was built with, got %q", ranListener)
    }

    if 128 != subscribed.Config().Priority {
        t.Fatalf("unexpected priority: %d", subscribed.Config().Priority)
    }
}

func TestNewSubscribedEvent_ANegativePriorityTravelsUnchanged(t *testing.T) {
    ranListener := ""
    subscribed := NewSubscribedEvent(newSubscriberProbeListener(&ranListener, "late"), eventcontract.ListenerConfig{Priority: -64})

    if -64 != subscribed.Config().Priority {
        t.Fatalf("expected a negative priority to be kept, got %d", subscribed.Config().Priority)
    }
}
