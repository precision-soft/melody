package contract

import (
    containercontract "github.com/precision-soft/melody/container/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope — one http request, one command run. What it registers is built on the first resolution through a scope and closed when that scope closes, so a service may hold the request it was built for without holding it for the life of the process.

It is a separate interface rather than a second method on ServiceModule because every module that exists implements the latter, and a hook nobody asked for must not be a compile break. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(kernelInstance kernelcontract.Kernel, registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar registers the services a scope owns.

It shares no method with ServiceRegistrar on purpose. The two registrations differ only in lifetime, and a provider handed to the wrong one is a mistake the compiler cannot see when both spell the same verb: a container provider registered as scoped is rebuilt and torn down once per request without ever failing. A container provider handed to this hook, or a scoped provider handed to RegisterServices, is therefore a compile error at the call site. */
type ScopedServiceRegistrar interface {
    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
