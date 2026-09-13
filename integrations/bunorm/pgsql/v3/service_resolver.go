package pgsql

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/uptrace/bun"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

/* RegisterLockerService publishes the PostgreSQL advisory-lock Locker under the framework's own locker service name, so an application that wires this integration gets the same door lock.LockerMustFromContainer already reads — the mysql sibling has had this since the plug-and-play pass and this module went without it, which is why every consumer had to construct NewLocker by hand and name the service itself.

   The name is the framework's, not this package's: an application resolves "the locker" and gets whichever backend it wired, which is what makes the choice a matter of configuration rather than of code. A process that wires two backends registers the second one under a name of its own instead of calling this twice. */
func RegisterLockerService(registrar ServiceRegistrar, database *bun.DB) {
    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewLocker(database), nil
        },
    )
}
