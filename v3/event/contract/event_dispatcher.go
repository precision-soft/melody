package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type ListenerRegistration struct {
    EventName  string
    ListenerId uint64
}

/* SubscriberRegistration identifies one installation of a subscriber, the way ListenerRegistration identifies one registration of a listener. It exists because the subscriber value cannot identify itself: a subscriber that carries no fields occupies no memory, and every zero-size allocation in Go answers one address, so two instances of such a type are one pointer and nothing in the value can ever tell them apart. Filed under the value, a removal for either instance took both instances' listeners down and reported a plausible count for it. The id is the dispatcher's own, issued at installation and unique for the life of the dispatcher, so two installations of one subscriber — of one zero-size subscriber included — are two registrations that are removed independently. */
type SubscriberRegistration struct {
    SubscriberId uint64
}

/* RequiredListenerRegistrar is an optional interface an EventDispatcher may implement to mark registered listeners as required. When a listener stops event propagation before a required listener behind it (lower priority) has run, Dispatch/DispatchName return an error instead of silently completing, so a caller such as the http kernel fails closed rather than proceeding as if the required listener — for example the security access-control listener — had run. A listener that legitimately short-circuits past required listeners opts out through MarkListenerMaySkipRequiredListeners. Both marks default off, so a dispatcher and its callers behave exactly as before unless a listener is explicitly marked. A listener error also ends the dispatch; when a required listener sits behind the failing one, the returned error reports the skip and carries the failure as its cause. */
type RequiredListenerRegistrar interface {
    MarkListenerRequired(registration ListenerRegistration)

    MarkListenerMaySkipRequiredListeners(registration ListenerRegistration)
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

    RemoveListener(registration ListenerRegistration) bool

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them. Hold the registration to remove them: the subscriber value is not accepted back, because it cannot identify which installation to undo. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes the listeners installed by one AddSubscriber call and answers how many were removed. An unknown registration removes nothing and answers zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener error aborts the remaining listeners and is returned alongside the (partially dispatched) event; callers decide the policy for partial dispatch. */
    Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for the given event name and payload: listeners run in descending priority order and the first listener error aborts the remaining listeners. */
    DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
