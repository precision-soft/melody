package config

import (
    melodyconfig "github.com/precision-soft/melody/config"
    melodyconfigcontract "github.com/precision-soft/melody/config/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

/* newConfigurationResolver hands an integration provider a resolver that can answer for the configuration, which the application's own container cannot do yet while the modules are being wired.

Boot runs the module hooks before it registers the framework's services, so `service.config` does not exist while RegisterServices and RegisterHttpRoutes run. A provider that resolves the configuration through the application container at that point fails on a service that is not there yet, and the failure reads like a broken .env rather than like the ordering it is. A throwaway container holding the single service the providers ask for is the smallest honest answer: it is not the application's container, nothing else is ever resolved through it, and it goes away with the wiring.

Nothing here is needed for a service that opens lazily — see the database, which defers its connection to the first resolution and therefore runs after the framework's services are in place. */
func newConfigurationResolver(kernelInstance melodykernelcontract.Kernel) melodycontainercontract.Resolver {
    bootstrapContainer := melodycontainer.NewContainer()

    bootstrapContainer.MustRegister(
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return kernelInstance.Config(), nil
        },
    )

    return bootstrapContainer
}

/* parameterValue reads a configuration parameter as a string, answering with an empty string for a parameter that was never registered. An unset endpoint is how the example decides an integration is not wired at all, so the absent case has to be an answer rather than a panic. */
func parameterValue(kernelInstance melodykernelcontract.Kernel, parameterName string) string {
    configuration := kernelInstance.Config()
    if nil == configuration {
        return ""
    }

    parameter := configuration.Get(parameterName)
    if nil == parameter {
        return ""
    }

    return parameter.String()
}
