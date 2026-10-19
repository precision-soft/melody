package config

import (
    melodyconfig "github.com/precision-soft/melody/v2/config"
    melodyconfigcontract "github.com/precision-soft/melody/v2/config/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

/* newConfigurationResolver hands an integration provider a resolver that answers for the configuration while the modules are wired: boot runs the module hooks before it registers `service.config`, so the application container cannot answer yet. The throwaway container holds that single service and goes away with the wiring; a service that opens lazily, such as the database, needs none of it. */
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
