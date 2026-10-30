package pgsql

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/uptrace/bun"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* RegisterLockerService publishes the PostgreSQL advisory-lock Locker under the framework's own locker service name, the one lock.LockerMustFromContainer reads, so the backend is a matter of configuration. It is built with config when it is first resolved; the configuration is resolved here, so a ReleaseTimeout outside its rule panics at the wiring, naming the field, not at the first lock. A process wiring two backends registers the second under a name of its own. */
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
