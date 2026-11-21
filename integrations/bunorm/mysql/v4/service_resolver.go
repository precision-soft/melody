package mysql

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/uptrace/bun"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* RegisterLockerService publishes the MySQL Locker under the framework's own locker service name, built with config when it is first resolved; the configuration is resolved here, so a ReleaseTimeout outside its rule panics at the wiring, naming the field, not at the first lock. */
func RegisterLockerService(registrar ServiceRegistrar, database *bun.DB, config LockerConfig) {
    releaseTimeout := mustResolveLockerReleaseTimeout(config)

    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return newLocker(database, releaseTimeout), nil
        },
        containercontract.RegisterOptions{},
    )
}
