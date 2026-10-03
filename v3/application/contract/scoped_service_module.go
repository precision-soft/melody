package contract

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ScopedServiceModule is the hook for services whose lifetime is one scope: one http request, one message delivery, one cron tick. What it registers is built on the first resolution through a scope and closed with that scope. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar is also a container scoped registrar, so a module can register through container.MustRegisterScopedType and the generated wiring as well as by name. It does not embed containercontract.Registrar, nor ServiceRegistrar the scoped one, so handing a provider to the wrong lifetime is a compile error. */
type ScopedServiceRegistrar interface {
    containercontract.ScopedRegistrar

    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
