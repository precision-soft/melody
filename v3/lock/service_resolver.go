package lock

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

const ServiceLocker = "service.lock.locker"

func LockerMustFromContainer(serviceContainer containercontract.Container) lockcontract.Locker {
    return container.MustFromResolver[lockcontract.Locker](serviceContainer, ServiceLocker)
}

func LockerMustFromResolver(resolver containercontract.Resolver) lockcontract.Locker {
    return container.MustFromResolver[lockcontract.Locker](resolver, ServiceLocker)
}
