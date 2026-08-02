package event

import (
    "testing"

    eventcontract "github.com/precision-soft/melody/event/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func newSubscriberProbeListener(recorder *string, name string) eventcontract.EventListener {
    return func(runtimeInstance runtimecontract.Runtime, eventInstance eventcontract.Event) error {
        *recorder = name

        return nil
    }
}

/* @info a subscription is the pair the dispatcher orders its listeners by, and neither half had ever been read back: a constructor that dropped the priority would file every listener at zero and the whole ordering — which is what makes a security listener run before the router's — would collapse silently into registration order. */
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

/* @info a negative priority is a legitimate declaration — it is how a listener asks to run after the defaults — so it must travel unchanged rather than being clamped into the zero the constructor would otherwise be free to assume. */
func TestNewSubscribedEvent_ANegativePriorityTravelsUnchanged(t *testing.T) {
    ranListener := ""
    subscribed := NewSubscribedEvent(newSubscriberProbeListener(&ranListener, "late"), -64)

    if -64 != subscribed.Priority() {
        t.Fatalf("expected a negative priority to be kept, got %d", subscribed.Priority())
    }
}
