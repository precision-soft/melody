package contract

import (
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* RequiredListenerRegistrar is an optional interface an EventDispatcher may implement to mark registered listeners as required. When a listener stops event propagation before a required listener behind it (lower priority) has run, Dispatch/DispatchName return an error instead of silently completing, so a caller such as the http kernel fails closed rather than proceeding as if the required listener — for example the security access-control listener — had run. A listener that legitimately short-circuits past required listeners opts out through MarkListenerMaySkipRequiredListeners. Both marks default off, so a dispatcher and its callers behave exactly as before unless a listener is explicitly marked; the first listener error already aborts dispatch regardless. */
type RequiredListenerRegistrar interface {
    MarkListenerRequired(registration ListenerRegistration)

    MarkListenerMaySkipRequiredListeners(registration ListenerRegistration)
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

    RemoveListener(registration ListenerRegistration) bool

    AddSubscriber(subscriber EventSubscriber)

    RemoveSubscriber(subscriber EventSubscriber) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener error aborts the remaining listeners and is returned alongside the (partially dispatched) event; callers decide the policy for partial dispatch. */
    Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for the given event name and payload: listeners run in descending priority order and the first listener error aborts the remaining listeners. */
    DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
