package contract

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ScopedServiceModule registers services built on first resolution and closed with their scope, such as one request, message delivery or cron tick. */
type ScopedServiceModule interface {
    Module

    RegisterScopedServices(registrar ScopedServiceRegistrar)
}

/* ScopedServiceRegistrar supports named and typed scoped registrations. Its method set is disjoint from ServiceRegistrar so the compiler rejects a registrar with the wrong lifetime. */
type ScopedServiceRegistrar interface {
    containercontract.ScopedRegistrar

    RegisterScopedService(
        serviceName string,
        provider any,
        options ...containercontract.RegisterOption,
    )
}
