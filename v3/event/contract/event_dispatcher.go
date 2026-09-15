package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* SubscriberRegistration uniquely identifies one installation for the dispatcher’s lifetime. Retain it for removal; subscriber pointer identity cannot distinguish repeated installations or zero-sized values. */
type SubscriberRegistration struct {
    SubscriberId uint64
}

/* RequiredListenerRegistrar marks listeners whose omission must make dispatch fail. Stopping propagation before a required listener returns a skip error unless the stopper explicitly opts out. A listener failure never receives that opt-out: the skip error retains the failure as its cause. Both marks default off. */
type RequiredListenerRegistrar interface {
    MarkListenerRequired(registration ListenerRegistration)

    MarkListenerMaySkipRequiredListeners(registration ListenerRegistration)
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

    RemoveListener(registration ListenerRegistration) bool

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them. Hold the registration to remove them: the subscriber value is not accepted back, because it cannot identify which installation to undo. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes listeners belonging to one AddSubscriber registration and returns their count. Unknown registrations return zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener error aborts the remaining listeners and is returned alongside the (partially dispatched) event; callers decide the policy for partial dispatch. */
    Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for the given event name and payload: listeners run in descending priority order and the first listener error aborts the remaining listeners. */
    DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
