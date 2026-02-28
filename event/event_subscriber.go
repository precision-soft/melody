package event

import (
    eventcontract "github.com/precision-soft/melody/event/contract"
)

func NewSubscribedEvent(
    listener eventcontract.EventListener,
    priority int,
) *SubscribedEvent {
    return &SubscribedEvent{
        listener: listener,
        priority: priority,
    }
}

type SubscribedEvent struct {
    listener eventcontract.EventListener
    priority int
}

func (instance *SubscribedEvent) Listener() eventcontract.EventListener {
    return instance.listener
}

func (instance *SubscribedEvent) Priority() int {
    return instance.priority
}

var _ eventcontract.SubscribedEvent = (*SubscribedEvent)(nil)
