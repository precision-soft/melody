package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* SubscriberRegistration identifies one installation of a subscriber, as ListenerRegistration identifies one registration of a listener. The subscriber value cannot identify itself, since every zero-size value shares one address; the id is issued by the dispatcher and unique for its life, so two installations of one subscriber are removed independently. */
type SubscriberRegistration struct {
    SubscriberId uint64
}

/* RequiredListenerRegistrar is an optional interface an EventDispatcher may implement to mark registered listeners as required. When a listener stops propagation or fails before a required listener behind it has run, the dispatch returns an error, so a caller such as the http kernel fails closed; a listener that legitimately short-circuits opts out through MarkListenerMaySkipRequiredListeners. Both marks default off, and a failure travels as the cause of the refusal. */
type RequiredListenerRegistrar interface {
    MarkListenerRequired(registration ListenerRegistration)

    MarkListenerMaySkipRequiredListeners(registration ListenerRegistration)
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

    RemoveListener(registration ListenerRegistration) bool

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them; hold it to remove them. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes the listeners installed by one AddSubscriber call and answers how many were removed. An unknown registration removes nothing and answers zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener error aborts the remaining listeners and is returned alongside the (partially dispatched) event; callers decide the policy for partial dispatch. */
    Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for the given event name and payload: listeners run in descending priority order and the first listener error aborts the remaining listeners. */
    DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
