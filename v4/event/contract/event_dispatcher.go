package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* SubscriberRegistration identifies one installation of a subscriber, as ListenerRegistration identifies one registration of a listener: AddSubscriber answers it and RemoveSubscriber takes it. The subscriber value cannot identify itself, since every zero-size value shares one address; the id is issued by the dispatcher and unique for its life, so two installations of one subscriber are removed independently. */
type SubscriberRegistration struct {
    SubscriberId uint64
}

/* RequiredListenerRegistrar is an interface an EventDispatcher implements to mark registered listeners as required; the security firewall refuses a kernel dispatcher without it. When a listener stops propagation or fails before a required listener behind it has run, the dispatch returns an error, so a caller such as the http kernel fails closed; a listener that legitimately short-circuits opts out through MarkListenerMaySkipRequiredListeners. Both marks default off, and a failure travels as the cause of the refusal. */
type RequiredListenerRegistrar interface {
    MarkListenerRequired(registration ListenerRegistration)

    MarkListenerMaySkipRequiredListeners(registration ListenerRegistration)
}

/* PriorityFloorRegistrar is an interface an EventDispatcher implements to give the bottom of one event's order to the listener that answers last, registered and reserved as one step: a later registration on that event at or below the priority could never run, so it is refused by name. A registration made before stands, and the order is not changed. A second installation on an event already holding a floor adds nothing and answers false, so kernels sharing a dispatcher install one listener. The http kernel refuses, when it builds its handler, a dispatcher without it. */
type PriorityFloorRegistrar interface {
    InstallFloorListener(eventName string, listener EventListener, priority int) (ListenerRegistration, bool)
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

    RemoveListener(registration ListenerRegistration) bool

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them; hold it to remove them. A value subscriber is installable, and one subscriber installed twice is two installations. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes the listeners installed by one AddSubscriber call and answers how many were removed. An unknown registration removes nothing and answers zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener error aborts the remaining listeners and is returned alongside the (partially dispatched) event, as the cause of a RequiredListenerSkippedError when a listener marked required stood behind it; callers decide the policy for partial dispatch. */
    Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for the given event name and payload: listeners run in descending priority order and the first listener error aborts the remaining listeners. */
    DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
