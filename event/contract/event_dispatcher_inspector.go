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
}

const (
	RegisteredListenerSourceListener   = "listener"
	RegisteredListenerSourceSubscriber = "subscriber"
)
