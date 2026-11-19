package clock

import (
    "context"

    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    ServiceClock = "service.clock"
)

func ClockMustFromResolver(ctx context.Context, resolver containercontract.Resolver) clockcontract.Clock {
    if true == internal.IsNilInterface(resolver) {
        exception.Panic(
            exception.NewError("resolver may not be nil", nil, nil),
        )
    }

    return container.MustFromResolver[clockcontract.Clock](ctx, resolver, ServiceClock)
}
