package event

import (
    "testing"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newSubscriberProbeListener(recorder *string, name string) eventcontract.EventListener {
    return func(runtimeInstance runtimecontract.Runtime, eventInstance eventcontract.Event) error {
        *recorder = name

        return nil
    }
}

func TestNewSubscribedEvent_CarriesTheListenerAndItsPriority(t *testing.T) {
    ranListener := ""
    listener := newSubscriberProbeListener(&ranListener, "probe")

    subscribed := NewSubscribedEvent(listener, 128)

    /* a listener is a function value, which Go refuses to compare, so the subscription is asked to
       produce it and the produced one is asked to run: that is the only way to say it is the same one. */
    if runErr := subscribed.Listener()(nil, nil); nil != runErr {
        t.Fatalf("unexpected listener error: %v", runErr)
    }

    if "probe" != ranListener {
        t.Fatalf("expected the subscription to carry the listener it was built with, got %q", ranListener)
    }

    if 128 != subscribed.Priority() {
        t.Fatalf("unexpected priority: %d", subscribed.Priority())
    }
}

func TestNewSubscribedEvent_ANegativePriorityTravelsUnchanged(t *testing.T) {
    ranListener := ""
    subscribed := NewSubscribedEvent(newSubscriberProbeListener(&ranListener, "late"), -64)

    if -64 != subscribed.Priority() {
        t.Fatalf("expected a negative priority to be kept, got %d", subscribed.Priority())
    }
}
