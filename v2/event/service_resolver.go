package event

import (
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
	eventcontract "github.com/precision-soft/melody/v2/event/contract"
)

const (
	ServiceEventDispatcher = "service.event.dispatcher"
)

func EventDispatcherMustFromContainer(serviceContainer containercontract.Container) eventcontract.EventDispatcher {
	return container.MustFromResolver[eventcontract.EventDispatcher](serviceContainer, ServiceEventDispatcher)
}

func EventDispatcherMustFromResolver(resolver containercontract.Resolver) eventcontract.EventDispatcher {
	return container.MustFromResolver[eventcontract.EventDispatcher](resolver, ServiceEventDispatcher)
}
