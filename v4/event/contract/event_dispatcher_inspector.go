package contract

type RegisteredEvent struct {
    EventName string `json:"eventName"`

    /* Declared reports that an owner declared the event; RunAfterFailureTags is what it declared. */
    Declared            bool     `json:"declared"`
    RunAfterFailureTags []string `json:"runAfterFailureTags"`

    Listeners []RegisteredListener `json:"listeners"`
}

type RegisteredListener struct {
    Priority     int    `json:"priority"`
    Source       string `json:"source"`
    Owner        string `json:"owner"`
    ListenerId   string `json:"listenerId"`
    ListenerName string `json:"listenerName"`

    /* Required and MaySkipRequiredListeners report what the listener's ListenerConfig declared, so an armed fail-closed guarantee is visible to inspection. */
    Required                 bool `json:"required"`
    MaySkipRequiredListeners bool `json:"maySkipRequiredListeners"`

    Tags []string `json:"tags"`

    /* RunsAfterFailure reports that the event declares one of the listener's tags, so the listener runs after a failure ahead of it. */
    RunsAfterFailure bool `json:"runsAfterFailure"`
}

const (
    RegisteredListenerSourceListener   = "listener"
    RegisteredListenerSourceSubscriber = "subscriber"
)
