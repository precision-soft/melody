package contract

import (
    containercontract "github.com/precision-soft/melody/container/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope, one http request or one command run: what it registers is built on the first resolution through a scope and closed with that scope. Like RegisterServices it runs before the framework's own container boot, so a scoped provider resolves what it needs at resolution time. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(kernelInstance kernelcontract.Kernel, registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar registers the services a scope owns. It shares no method with ServiceRegistrar, so calling the other lifetime's method is a compile error at the call site; both take the provider as any, so matching a provider to the lifetime it was written for stays the caller's. */
type ScopedServiceRegistrar interface {
    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
