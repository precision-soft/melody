package contract

type RegisteredEvent struct {
    EventName string `json:"eventName"`

    /* Declared reports that an owner declared the event; Config is what it declared, the zero EventConfig with an empty list when none did. */
    Declared bool        `json:"declared"`
    Config   EventConfig `json:"config"`

    Listeners []RegisteredListener `json:"listeners"`
}

type RegisteredListener struct {
    Source     string `json:"source"`
    Owner      string `json:"owner"`
    ListenerId string `json:"listenerId"`

    /* ListenerName is the name inspection and the journal use: Config.Name, or the function's qualified name when that is empty. */
    ListenerName string `json:"listenerName"`

    /* Config is what the registration declared, its tags a copy, so an armed fail-closed guarantee is visible to inspection. */
    Config ListenerConfig `json:"config"`

    /* RunsAfterFailure reports that the event declares one of the listener's tags, so the listener runs after a failure ahead of it. */
    RunsAfterFailure bool `json:"runsAfterFailure"`
}

const (
    RegisteredListenerSourceListener   = "listener"
    RegisteredListenerSourceSubscriber = "subscriber"
)
