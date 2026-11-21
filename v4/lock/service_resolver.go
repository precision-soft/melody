package lock

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

const ServiceLocker = "service.lock.locker"

func LockerMustFromRuntime(runtimeInstance containercontract.Runtime) lockcontract.Locker {
    return runtime.MustFromRuntime[lockcontract.Locker](runtimeInstance, ServiceLocker)
}

func LockerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) lockcontract.Locker {
    return container.MustFromResolver[lockcontract.Locker](ctx, resolver, ServiceLocker)
}
