package contract

import (
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
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
