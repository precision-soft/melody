package event

import (
    "sync"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

/* subscriberBookkeeping files the installations made through AddSubscriber, for EventDispatcher and EventDispatcherAdapter alike, each under an id it issues and each keeping a record R of its own per installed listener, so a value subscriber is installable, one subscriber installed twice is two installations, and RemoveSubscriber takes the registration AddSubscriber answered. */
type subscriberBookkeeping[R any] struct {
    /* subscriberMutex serializes whole installations and removals against each other, so a removal never interleaves with the installation it undoes; it is always taken before mutex and never inside it, and the owner's listener operations, which the installer and the remover run, never hold either. */
    subscriberMutex sync.Mutex
    /* mutex guards the map and the counter; the owner may hold its own mutex when it takes this one, never the reverse. */
    mutex         sync.RWMutex
    registrations map[uint64][]R
    /* nextId issues the identity AddSubscriber answers with. It is monotonic and never reused, so a registration held past its removal names a removal that already happened. */
    nextId uint64
}

/* subscriberListenerInstaller installs one planned listener of a subscriber and answers the record kept for it. */
type subscriberListenerInstaller[R any] func(planned plannedSubscriberRegistration, subscriberType string) R

/* subscriberListenerRemover removes the listener one record names and answers how many it removed. */
type subscriberListenerRemover[R any] func(record R) int

func newSubscriberBookkeeping[R any]() *subscriberBookkeeping[R] {
    return &subscriberBookkeeping[R]{
        registrations: make(map[uint64][]R),
    }
}

/* addSubscriber files the installation under an id this bookkeeping issues and answers the registration, the only handle on it. Every subscribed event is validated before a single listener is installed, so a malformed subscriber is never half-installed. */
func (instance *subscriberBookkeeping[R]) addSubscriber(subscriber eventcontract.EventSubscriber, install subscriberListenerInstaller[R]) eventcontract.SubscriberRegistration {
    subscriberType := requireEventSubscriber(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    subscriberId := instance.install(plannedList, subscriberType, install)

    return eventcontract.SubscriberRegistration{SubscriberId: subscriberId}
}

/* removeSubscriber removes the listeners one addSubscriber call installed and answers how many. An unknown registration, one already removed or the zero value, removes nothing and answers zero. */
func (instance *subscriberBookkeeping[R]) removeSubscriber(registration eventcontract.SubscriberRegistration, remove subscriberListenerRemover[R]) int {
    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    return instance.uninstall(registration.SubscriberId, remove)
}

/* install issues an installation id and installs the planned listeners under it. The caller holds subscriberMutex. */
func (instance *subscriberBookkeeping[R]) install(
    plannedList []plannedSubscriberRegistration,
    subscriberType string,
    install subscriberListenerInstaller[R],
) uint64 {
    instance.mutex.Lock()
    instance.nextId++
    subscriberId := instance.nextId
    instance.mutex.Unlock()

    for _, planned := range plannedList {
        record := install(planned, subscriberType)

        instance.mutex.Lock()
        instance.registrations[subscriberId] = append(instance.registrations[subscriberId], record)
        instance.mutex.Unlock()
    }

    return subscriberId
}

/* uninstall removes the listeners of one installation and answers how many. The caller holds subscriberMutex. */
func (instance *subscriberBookkeeping[R]) uninstall(subscriberId uint64, remove subscriberListenerRemover[R]) int {
    instance.mutex.Lock()
    registrationList := instance.registrations[subscriberId]
    delete(instance.registrations, subscriberId)
    instance.mutex.Unlock()

    removedCount := 0
    for _, record := range registrationList {
        removedCount = removedCount + remove(record)
    }

    return removedCount
}

/* dropListener scrubs the record of a listener removed on its own, through the owner's RemoveListener, from every installation, and drops an installation left empty, so no record outlives its listener. */
func (instance *subscriberBookkeeping[R]) dropListener(matches func(record R) bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for subscriberId, registrationList := range instance.registrations {
        filtered := make([]R, 0, len(registrationList))
        for _, record := range registrationList {
            if true == matches(record) {
                continue
            }

            filtered = append(filtered, record)
        }

        if 0 == len(filtered) {
            delete(instance.registrations, subscriberId)
            continue
        }

        instance.registrations[subscriberId] = filtered
    }
}

/* eachRecord visits every installation's records, a point-in-time view. */
func (instance *subscriberBookkeeping[R]) eachRecord(visit func(record R)) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    for _, registrationList := range instance.registrations {
        for _, record := range registrationList {
            visit(record)
        }
    }
}
