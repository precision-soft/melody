package contract

import (
    containercontract "github.com/precision-soft/melody/container/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* ServiceModule registers process-lifetime services. The hook runs before the framework's own container boot, so no framework service is resolvable inside it — Has answers false even for the logger, because the framework claims a default only for a name no module registered. A provider registered here resolves its dependencies at resolution time, when the container is complete. */
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
