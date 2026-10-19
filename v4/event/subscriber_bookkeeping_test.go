package event

import (
    "runtime"
    "testing"
    "weak"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* detachedListenerSubscriber answers listeners that do not capture it, the example's idiom, so nothing but the bookkeeping holds it once the application drops its pointer; the payload keeps it off the zero-size address */
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
    AddSubscriber(subscriber eventcontract.EventSubscriber)
    RemoveSubscriber(subscriber eventcontract.EventSubscriber) int
}

/* installs a subscriber and drops the only strong reference the caller held, answering the weak one */
func installDetachedSubscriber(registrar subscriberRegistrar) weak.Pointer[detachedListenerSubscriber] {
    subscriber := &detachedListenerSubscriber{}
    registrar.AddSubscriber(subscriber)

    return weak.Make(subscriber)
}

/* a subscriber installed through AddSubscriber stays reachable while installed, whatever the application holds: collected, its address could be handed to a fresh value of its type, which AddSubscriber would refuse as already registered and whose RemoveSubscriber would take the orphan's listeners down; removed, nothing holds it */
func TestSubscriberBookkeeping_KeepsAnInstalledSubscriberReachable(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    for name, registrar := range map[string]subscriberRegistrar{
        "dispatcher": dispatcher,
        "adapter":    NewEventDispatcherAdapter(dispatcher, clockInstance),
    } {
        t.Run(name, func(t *testing.T) {
            pointer := installDetachedSubscriber(registrar)

            runtime.GC()
            runtime.GC()

            installed := pointer.Value()
            if nil == installed {
                t.Fatalf("expected the installed subscriber to stay reachable through the bookkeeping")
            }

            if 1 != registrar.RemoveSubscriber(installed) {
                t.Fatalf("expected the one listener removed")
            }
            installed = nil

            runtime.GC()
            runtime.GC()

            if nil != pointer.Value() {
                t.Fatalf("expected the removed subscriber to be released")
            }
        })
    }
}

/* the pointer door files the subscriber beside its identity; the registration door files no pointer */
func TestSubscriberBookkeeping_FilesTheSubscriberOfAPointerInstallation(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &detachedListenerSubscriber{}
    dispatcher.AddSubscriber(subscriber)

    subscriberId := dispatcher.subscribers.idByIdentity[eventSubscriberIdentity(subscriber)]
    if subscriber != dispatcher.subscribers.installationById[subscriberId].subscriber {
        t.Fatalf("expected the installation to hold the subscriber")
    }

    registration := dispatcher.AddSubscriberWithRegistration(&detachedListenerSubscriber{})
    if _, filed := dispatcher.subscribers.installationById[registration.SubscriberId]; true == filed {
        t.Fatalf("expected the registration door to file no pointer")
    }

    dispatcher.RemoveSubscriber(subscriber)
    if 0 != len(dispatcher.subscribers.installationById) || 0 != len(dispatcher.subscribers.idByIdentity) {
        t.Fatalf("expected the pointer installation forgotten and the registration one never filed, got %d %d", len(dispatcher.subscribers.installationById), len(dispatcher.subscribers.idByIdentity))
    }
}
