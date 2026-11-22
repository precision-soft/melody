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

/* ListenerConfig is what a registration declares about its listener; the zero value is priority 0, unnamed, untagged, neither required nor allowed to skip, replacing nothing after a failure. */
type ListenerConfig struct {
    /* Priority orders the listeners of one event, the highest first; on InstallFloorListener it is the reserved floor. */
    Priority int

    /* Name is what inspection and the journal call the listener; empty takes the function's qualified name. */
    Name string

    /* Required makes a dispatch that ends before this listener ran, by a stop or by a listener's failure, answer a RequiredListenerSkippedError, so a caller such as the http kernel fails closed. */
    Required bool

    /* MaySkipRequiredListeners lets this listener stop propagation ahead of required listeners without the refusal; it is not read when the listener failed. */
    MaySkipRequiredListeners bool

    /* Tags name what the listener is, in its event owner's vocabulary ("cache", "audit"). An event that declares one of them in EventConfig.RunAfterFailureTags runs this listener after a listener ahead of it failed; on an event that declares none, tags change nothing. A tag is non-empty, carries no whitespace or control character and appears once. */
    Tags []string

    /* ReplacesResponseAfterFailure lets this listener, when it runs after a failure through its tags, replace the response of a payload that guards it; the http kernel's kernel.response payload does. Without it such a replacement is ignored and the response standing at the failure is served; either way the kernel writes a warning naming the listener. It is not read in a dispatch nothing failed in, nor on kernel.request, kernel.controller and kernel.exception, where the kernel answers a failed dispatch itself. */
    ReplacesResponseAfterFailure bool
}

/* EventConfig is what the owner of an event name declares about its dispatch; the zero value declares nothing: the first failure ends the dispatch and no listener behind it runs. */
type EventConfig struct {
    /* RunAfterFailureTags are the listener tags that still run once a listener failed: every listener behind the failed one carrying at least one of them runs, in priority order, reads the failure through Event.PriorFailure and has its own failure joined to it. A deliberate stop runs none of them. */
    RunAfterFailureTags []string
}

type EventDispatcher interface {
    AddListener(eventName string, listener EventListener, config ListenerConfig) ListenerRegistration

    /* InstallFloorListener gives the bottom of one event's order to the listener that answers last, registered and reserved as one step at config.Priority: a later registration on that event at or below the priority could never run, so it is refused by name. A registration made before stands, and the order is not changed. A second installation on an event already holding a floor adds nothing and answers false, so kernels sharing a dispatcher install one listener. */
    InstallFloorListener(eventName string, listener EventListener, config ListenerConfig) (ListenerRegistration, bool)

    RemoveListener(registration ListenerRegistration) bool

    /* DeclareEvent records what the owner of the event name declares. An event is declared once: a second declaration of the name is refused by name, whatever it carries, so two owners cannot disagree in silence. Listeners may be registered before or after it; a dispatch reads the declaration standing when it starts. */
    DeclareEvent(eventName string, config EventConfig)

    /* AddSubscriber installs every listener the subscriber declares and answers the registration that owns them; hold it to remove them. A value subscriber is installable, and one subscriber installed twice is two installations. */
    AddSubscriber(subscriber EventSubscriber) SubscriberRegistration

    /* RemoveSubscriber removes the listeners installed by one AddSubscriber call and answers how many were removed. An unknown registration removes nothing and answers zero. */
    RemoveSubscriber(registration SubscriberRegistration) int

    /* Dispatch runs the listeners registered for the event's name in descending priority order. The first listener that fails, by a returned error or a recovered panic, ends the dispatch, as a stop does; only the listeners behind it carrying a tag the event declared in EventConfig.RunAfterFailureTags still run. The answer is that failure, joined in run order with the failures of the tagged listeners, each reachable through errors.Is and errors.As, and is the cause of a RequiredListenerSkippedError when a required listener did not run. */
    Dispatch(runtimeInstance containercontract.Runtime, event Event) (Event, error)

    /* DispatchName behaves like Dispatch for an event of the name and payload, stamped with the dispatcher's clock. */
    DispatchName(runtimeInstance containercontract.Runtime, eventName string, payload any) (Event, error)

    /* RegisteredEvents reports a point-in-time view of the registered listeners and what each declared; dispatch never depends on it. */
    RegisteredEvents() []RegisteredEvent
}
