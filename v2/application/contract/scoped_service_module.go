package contract

import (
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope, one http request or one command run: what it registers is built on the first resolution through a scope and closed with that scope. Like RegisterServices it runs before the framework's own container boot, so a scoped provider resolves what it needs at resolution time. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(kernelInstance kernelcontract.Kernel, registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar registers the services a scope owns. It shares no method with ServiceRegistrar, so a provider handed to the wrong lifetime is a compile error at the call site. */
type ScopedServiceRegistrar interface {
    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
