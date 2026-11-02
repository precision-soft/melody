package lock

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

const ServiceLocker = "service.lock.locker"

func LockerMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) lockcontract.Locker {
    return container.MustFromResolver[lockcontract.Locker](ctx, serviceContainer, ServiceLocker)
}

func LockerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) lockcontract.Locker {
    return container.MustFromResolver[lockcontract.Locker](ctx, resolver, ServiceLocker)
}
