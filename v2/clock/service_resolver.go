package clock

import (
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
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
