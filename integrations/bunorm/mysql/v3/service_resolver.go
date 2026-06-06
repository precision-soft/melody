package mysql

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/uptrace/bun"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterLockerService(registrar ServiceRegistrar, database *bun.DB) {
    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewLocker(database), nil
        },
    )
}
