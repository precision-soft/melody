package clock

import (
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

const (
    ServiceClock = "service.clock"
)

func ClockMustFromContainer(serviceContainer containercontract.Container) clockcontract.Clock {
    /* a nil container dereferences inside the container package and the panic blames the container instead of naming the argument this helper was handed */
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
