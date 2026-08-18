package contract

import (
    containercontract "github.com/precision-soft/melody/container/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope — one http request, one command run. What it registers is built on the first resolution through a scope and closed when that scope closes, so a service may hold the request it was built for without holding it for the life of the process. A long-running command that processes many units of work under its one run scope creates a child runtime per unit — a fresh scope, a runtime over it, closed with the unit, the cron runner's shape — so what is registered here stays unit-lifetime there too.

It is a separate interface rather than a second method on ServiceModule because every module that exists implements the latter, and a hook nobody asked for must not be a compile break.

Like RegisterServices, the hook runs before the framework's own container boot: no framework service is resolvable inside it, and a scoped provider that needs one resolves it at resolution time, through the scope it is built in. */
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
