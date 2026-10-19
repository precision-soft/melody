package event

import (
    "sync"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* subscriberBookkeeping files the installations made through the four subscriber doors, for EventDispatcher and EventDispatcherAdapter alike, each keeping a record R of its own per installed listener: under the subscriber's pointer for AddSubscriber and RemoveSubscriber, under an issued id for AddSubscriberWithRegistration and RemoveSubscriberRegistration. An installation filed under a pointer keeps the subscriber itself, so one the application dropped stays reachable while installed: collected, its address could be handed to a fresh value of its type, which this bookkeeping would refuse as already registered and whose removal would take the orphan's listeners down. */
type subscriberBookkeeping[R any] struct {
    /* subscriberMutex serializes whole installations and removals against each other, so a removal never interleaves with the installation it undoes; it is always taken before mutex and never inside it, and the owner's listener operations, which the installer and the remover run, never hold either. */
    subscriberMutex sync.Mutex
    /* mutex guards the maps and the counter; the owner may hold its own mutex when it takes this one, never the reverse. */
    mutex            sync.RWMutex
    registrations    map[uint64][]R
    idByIdentity     map[subscriberIdentity]uint64
    installationById map[uint64]subscriberInstallation
    /* nextId issues the identity AddSubscriberWithRegistration answers with. It is monotonic and never reused, so a registration held past its removal names a removal that already happened. */
    nextId uint64
}

/* subscriberInstallation is what a pointer-filed installation keeps: the identity it is filed under and the subscriber, held so it stays reachable while installed. */
type subscriberInstallation struct {
    identity   subscriberIdentity
    subscriber eventcontract.EventSubscriber
}

/* subscriberListenerInstaller installs one planned listener of a subscriber and answers the record kept for it. */
type subscriberListenerInstaller[R any] func(planned plannedSubscriberRegistration, subscriberType string) R

/* subscriberListenerRemover removes the listener one record names and answers how many it removed. */
type subscriberListenerRemover[R any] func(record R) int

func newSubscriberBookkeeping[R any]() *subscriberBookkeeping[R] {
    return &subscriberBookkeeping[R]{
        registrations:    make(map[uint64][]R),
        idByIdentity:     make(map[subscriberIdentity]uint64),
        installationById: make(map[uint64]subscriberInstallation),
    }
}

/* addSubscriber is the pointer door: a nil or a value subscriber is refused, and so is a second installation of one pointer, since every zero-size value shares one address and removeSubscriber would take both installations down. Every subscribed event is validated before a single listener is installed, so a malformed subscriber is never half-installed. */
func (instance *subscriberBookkeeping[R]) addSubscriber(subscriber eventcontract.EventSubscriber, install subscriberListenerInstaller[R]) {
    subscriberIdentityValue, subscriberType := requireEventSubscriberIdentity(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.RLock()
    _, alreadyRegistered := instance.idByIdentity[subscriberIdentityValue]
    instance.mutex.RUnlock()

    if true == alreadyRegistered {
        exception.Panic(
            exception.NewError(
                "event subscriber is already registered",
                exceptioncontract.Context{
                    "subscriberType": subscriberType,
                },
                nil,
            ),
        )
    }

    instance.install(
        plannedList,
        subscriberType,
        &subscriberInstallation{identity: subscriberIdentityValue, subscriber: subscriber},
        install,
    )
}

/* removeSubscriber removes every listener addSubscriber installed for the subscriber's pointer and answers how many; a pointer addSubscriber did not install removes nothing and answers zero. */
func (instance *subscriberBookkeeping[R]) removeSubscriber(subscriber eventcontract.EventSubscriber, remove subscriberListenerRemover[R]) int {
    subscriberIdentityValue, _ := requireEventSubscriberIdentity(
        subscriber,
        "remove a subscriber",
    )

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    instance.mutex.RLock()
    subscriberId, exists := instance.idByIdentity[subscriberIdentityValue]
    instance.mutex.RUnlock()

    if false == exists {
        return 0
    }

    return instance.uninstall(subscriberId, remove)
}

/* addSubscriberWithRegistration is the registration door: the installation is filed under an id this bookkeeping issues, so a value subscriber is installable and one subscriber twice, and the registration answered is the only handle on it. */
func (instance *subscriberBookkeeping[R]) addSubscriberWithRegistration(subscriber eventcontract.EventSubscriber, install subscriberListenerInstaller[R]) eventcontract.SubscriberRegistration {
    subscriberType := requireEventSubscriber(
        subscriber,
        "add a subscriber",
    )

    plannedList := planSubscriberRegistrations(subscriber)

    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    subscriberId := instance.install(plannedList, subscriberType, nil, install)

    return eventcontract.SubscriberRegistration{SubscriberId: subscriberId}
}

/* removeSubscriberRegistration removes the listeners one addSubscriberWithRegistration call installed and answers how many. An unknown registration, one already removed or the zero value, removes nothing and answers zero. */
func (instance *subscriberBookkeeping[R]) removeSubscriberRegistration(registration eventcontract.SubscriberRegistration, remove subscriberListenerRemover[R]) int {
    instance.subscriberMutex.Lock()
    defer instance.subscriberMutex.Unlock()

    return instance.uninstall(registration.SubscriberId, remove)
}

/* install issues an installation id and installs the planned listeners under it, filing the installation under the subscriber's pointer when one is given. The caller holds subscriberMutex. */
func (instance *subscriberBookkeeping[R]) install(
    plannedList []plannedSubscriberRegistration,
    subscriberType string,
    installation *subscriberInstallation,
    install subscriberListenerInstaller[R],
) uint64 {
    instance.mutex.Lock()
    instance.nextId++
    subscriberId := instance.nextId
    if nil != installation {
        instance.idByIdentity[installation.identity] = subscriberId
        instance.installationById[subscriberId] = *installation
    }
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
    instance.forget(subscriberId)
    instance.mutex.Unlock()

    removedCount := 0
    for _, record := range registrationList {
        removedCount = removedCount + remove(record)
    }

    return removedCount
}

/* forget drops the pointer an installation was filed under, so the pointer can be installed again. The caller holds mutex. */
func (instance *subscriberBookkeeping[R]) forget(subscriberId uint64) {
    installation, exists := instance.installationById[subscriberId]
    if false == exists {
        return
    }

    delete(instance.installationById, subscriberId)
    delete(instance.idByIdentity, installation.identity)
}

/* dropListener scrubs the record of a listener removed on its own, through the owner's RemoveListener, from every installation, and forgets an installation left empty, so no record outlives its listener. */
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
            instance.forget(subscriberId)
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
