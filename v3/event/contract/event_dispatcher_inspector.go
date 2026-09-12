package contract

type EventDispatcherInspector interface {
    RegisteredEvents() []RegisteredEvent
}

type RegisteredEvent struct {
    EventName string               `json:"eventName"`
    Listeners []RegisteredListener `json:"listeners"`
}

type RegisteredListener struct {
    Priority     int    `json:"priority"`
    Source       string `json:"source"`
    Owner        string `json:"owner"`
    ListenerId   string `json:"listenerId"`
    ListenerName string `json:"listenerName"`

    /* Required and MaySkipRequiredListeners report the RequiredListenerRegistrar marks the listener carries. Without them an unarmed fail-closed guarantee is invisible to inspection: the listener that is supposed to be required looks exactly like one that is not. */
    Required                 bool `json:"required"`
    MaySkipRequiredListeners bool `json:"maySkipRequiredListeners"`
}

const (
    RegisteredListenerSourceListener   = "listener"
    RegisteredListenerSourceSubscriber = "subscriber"
)
