package contract

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

    /* Required, MaySkipRequiredListeners and FailureEndsDispatch report what the listener's ListenerConfig declared, so an armed fail-closed guarantee is visible to inspection. */
    Required                 bool `json:"required"`
    MaySkipRequiredListeners bool `json:"maySkipRequiredListeners"`
    FailureEndsDispatch      bool `json:"failureEndsDispatch"`
}

const (
    RegisteredListenerSourceListener   = "listener"
    RegisteredListenerSourceSubscriber = "subscriber"
)
