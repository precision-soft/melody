package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* SubscriberRegistration identifies one installation of a subscriber, as ListenerRegistration identifies one registration of a listener: AddSubscriber answers it and RemoveSubscriber takes it. The subscriber value cannot identify itself, since every zero-size value shares one address; the id is issued by the dispatcher and unique for its life, so two installations of one subscriber are removed independently. */
type SubscriberRegistration struct {
    SubscriberId uint64
}

/* ListenerConfig is what a registration declares about its listener; the zero value is priority 0, unnamed, neither required nor allowed to skip. */
type ListenerConfig struct {
    /* Priority orders the listeners of one event, the highest first; on InstallFloorListener it is the reserved floor. */
    Priority int

    /* Name is what inspection and the journal call the listener; empty takes the function's qualified name. */
    Name string

    /* Required makes a dispatch that ends before this listener ran, by a stop or by a listener's failure, answer a RequiredListenerSkippedError, so a caller such as the http kernel fails closed. */
    Required bool

    /* MaySkipRequiredListeners lets this listener stop propagation ahead of required listeners without the refusal; it is not read when the listener failed. */
    MaySkipRequiredListeners bool
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, config ListenerConfig) ListenerRegistration

    /* InstallFloorListener gives the bottom of one event's order to the listener that answers last, registered and reserved as one step at config.Priority: a later registration on that event at or below the priority could never run, so it is refused by name. A registration made before stands, and the order is not changed. A second installation on an event already holding a floor adds nothing and answers false, so kernels sharing a dispatcher install one listener. */
    InstallFloorListener(eventName string, listener EventListener, config ListenerConfig) (ListenerRegistration, bool)

    RemoveListener(registration ListenerRegistration) bool

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them; hold it to remove them. A value subscriber is installable, and one subscriber installed twice is two installations. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes the listeners installed by one AddSubscriber call and answers how many were removed. An unknown registration removes nothing and answers zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener that fails, by a returned error or a recovered panic, ends the dispatch, as a stop does; the answer is that failure, reachable through errors.Is and errors.As, and is the cause of a RequiredListenerSkippedError when the dispatch ended before a required listener ran. */
    Dispatch(runtimeInstance containercontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for an event of the name and payload, stamped with the dispatcher's clock. */
    DispatchName(runtimeInstance containercontract.Runtime, eventName string, payload any) (Event, error)

    /* RegisteredEvents reports a point-in-time view of the registered listeners and what each declared; dispatch never depends on it. */
    RegisteredEvents() []RegisteredEvent
}
