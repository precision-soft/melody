package contract

import (
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type ListenerRegistration struct {
	EventName  string
	ListenerId uint64
}

type EventDispatcher interface {
	AddListener(eventName string, listener EventListener, priority int) ListenerRegistration

	RemoveListener(registration ListenerRegistration) bool

	AddSubscriber(subscriber EventSubscriber)

	RemoveSubscriber(subscriber EventSubscriber) int

	Dispatch(runtimeInstance runtimecontract.Runtime, event Event) (Event, error)

	DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (Event, error)
}
