package contract

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type ServiceModule interface {
    Module
    RegisterServices(registrar ServiceRegistrar)
}

/* ServiceRegistrar is also a container registrar, so a module can register through the container's typed helpers — container.MustRegisterType and the wiring generated on top of it — as well as by name. */
type ServiceRegistrar interface {
    containercontract.Registrar

    RegisterService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
