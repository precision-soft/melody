package config

import (
    "github.com/precision-soft/melody/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* RegisterScopedServices declares what one request owns: the change attribution is built on the first resolution through a request's scope and closed with it. The provider itself is the registration — it refuses any scope without a request context, which is how a console run, whose scope carries a process context instead, falls back to per-call attribution. */
func (instance *Module) RegisterScopedServices(
    kernelInstance melodykernelcontract.Kernel,
    registrar melodyapplicationcontract.ScopedServiceRegistrar,
) {
    registrar.RegisterScopedService(
        service.ServiceChangeAttribution,
        service.ChangeAttributionFromResolver,
    )
}

var _ melodyapplicationcontract.ScopedServiceModule = (*Module)(nil)
