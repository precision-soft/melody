package contract

type SubscribedEvent interface {
	Listener() EventListener

	Priority() int
}

type EventSubscriber interface {
	SubscribedEvents() map[string][]SubscribedEvent
}
