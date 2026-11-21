package contract

type SubscribedEvent interface {
    Listener() EventListener

    Config() ListenerConfig
}

type EventSubscriber interface {
    SubscribedEvents() map[string][]SubscribedEvent
}
