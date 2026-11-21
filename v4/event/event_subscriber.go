package event

import (
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

func NewSubscribedEvent(
    listener eventcontract.EventListener,
    config eventcontract.ListenerConfig,
) *SubscribedEvent {
    return &SubscribedEvent{
        listener: listener,
        config:   config,
    }
}

type SubscribedEvent struct {
    listener eventcontract.EventListener
    config   eventcontract.ListenerConfig
}

func (instance *SubscribedEvent) Listener() eventcontract.EventListener {
    return instance.listener
}

func (instance *SubscribedEvent) Config() eventcontract.ListenerConfig {
    return instance.config
}

var _ eventcontract.SubscribedEvent = (*SubscribedEvent)(nil)
