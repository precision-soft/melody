package clock

import (
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    ServiceClock = "service.clock"
)

func ClockMustFromContainer(serviceContainer containercontract.Container) clockcontract.Clock {
    if true == internal.IsNilInterface(serviceContainer) {
        exception.Panic(
            exception.NewError("container may not be nil", nil, nil),
        )
    }

    return container.MustFromResolver[clockcontract.Clock](serviceContainer, ServiceClock)
}

func ClockMustFromResolver(resolver containercontract.Resolver) clockcontract.Clock {
    if true == internal.IsNilInterface(resolver) {
        exception.Panic(
            exception.NewError("resolver may not be nil", nil, nil),
        )
    }

    return container.MustFromResolver[clockcontract.Clock](resolver, ServiceClock)
}
