package clock

import (
	clockcontract "github.com/precision-soft/melody/clock/contract"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
)

const (
	ServiceClock = "service.clock"
)

func ClockMustFromContainer(serviceContainer containercontract.Container) clockcontract.Clock {
	return container.MustFromResolver[clockcontract.Clock](serviceContainer, ServiceClock)
}

func ClockMustFromResolver(resolver containercontract.Resolver) clockcontract.Clock {
	return container.MustFromResolver[clockcontract.Clock](resolver, ServiceClock)
}
