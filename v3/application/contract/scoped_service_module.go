package contract

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope — one http request, one message delivery, one cron tick. What it registers is built on the first resolution through a scope and closed when that scope closes, so a service may hold the request it was built for without holding it for the life of the process.

It is a separate interface rather than a second method on ServiceModule because every module that exists implements the latter, and a hook nobody asked for must not be a compile break. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar is also a container scoped registrar, so a module can register through the typed helpers — container.MustRegisterScopedType and the wiring generated on top of it — as well as by name.

It deliberately does not embed containercontract.Registrar, and ServiceRegistrar deliberately does not embed containercontract.ScopedRegistrar. The two method sets are disjoint, so a container provider handed to this hook, or a scoped provider handed to RegisterServices, is a compile error at the call site rather than a lifetime mistake that only shows up as a service quietly rebuilt and closed once per request. */
type ScopedServiceRegistrar interface {
    containercontract.ScopedRegistrar

    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
