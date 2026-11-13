package event

import (
    "runtime"
    "testing"
    "weak"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* detachedListenerSubscriber answers listeners that do not capture it, the example's idiom, so nothing holds it once the application drops its pointer, the bookkeeping included; the payload keeps it off the zero-size address */
type detachedListenerSubscriber struct {
    payload [16]byte
}

func (instance *detachedListenerSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "detached": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

type subscriberRegistrar interface {
    AddSubscriber(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration
    RemoveSubscriber(registration eventcontract.SubscriberRegistration) int
}

/* installs a subscriber and drops the only strong reference the caller held, answering the weak one beside the registration */
func installDetachedSubscriber(registrar subscriberRegistrar) (weak.Pointer[detachedListenerSubscriber], eventcontract.SubscriberRegistration) {
    subscriber := &detachedListenerSubscriber{}
    registration := registrar.AddSubscriber(subscriber)

    return weak.Make(subscriber), registration
}

/* an installation is filed under its registration and holds no subscriber: one the application dropped, whose listeners do not capture it, is collected while installed, and its registration still removes the listeners it installed */
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
