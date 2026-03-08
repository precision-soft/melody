package contract

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type ServiceModule interface {
    Module
    RegisterServices(registrar ServiceRegistrar)
}

type ServiceRegistrar interface {
    RegisterService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
