package event

import (
    "runtime"
    "testing"
    "weak"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

/* the payload keeps it off the zero-size address every empty struct shares */
type detachedListenerSubscriber struct {
    payload [16]byte
}

func (instance *detachedListenerSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "detached": {
            NewSubscribedEvent(
                func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                eventcontract.ListenerConfig{},
            ),
        },
    }
}

type subscriberRegistrar interface {
    AddSubscriber(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration
    RemoveSubscriber(registration eventcontract.SubscriberRegistration) int
}

func installDetachedSubscriber(registrar subscriberRegistrar) (weak.Pointer[detachedListenerSubscriber], eventcontract.SubscriberRegistration) {
    subscriber := &detachedListenerSubscriber{}
    registration := registrar.AddSubscriber(subscriber)

    return weak.Make(subscriber), registration
}

func TestSubscriberBookkeeping_HoldsNoSubscriber(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    for name, registrar := range map[string]subscriberRegistrar{
        "dispatcher": dispatcher,
        "adapter":    NewEventDispatcherAdapter(dispatcher),
    } {
        t.Run(name, func(t *testing.T) {
            pointer, registration := installDetachedSubscriber(registrar)

            runtime.GC()
            runtime.GC()

            if nil != pointer.Value() {
                t.Fatalf("expected the dropped subscriber to be released while installed")
            }

            if 1 != registrar.RemoveSubscriber(registration) {
                t.Fatalf("expected the one listener removed through the registration")
            }
        })
    }
}
