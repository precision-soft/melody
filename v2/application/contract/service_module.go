package contract

import (
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type ServiceModule interface {
    Module
    RegisterServices(kernelInstance kernelcontract.Kernel, registrar ServiceRegistrar)
}

type ServiceRegistrar interface {
    RegisterService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
