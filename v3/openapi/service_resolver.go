package openapi

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

const ServiceOpenApiRegistry = "service.openapi.registry"

const ServiceOpenApiInfo = "service.openapi.info"

func RegistryMustFromContainer(serviceContainer containercontract.Container) *Registry {
    return container.MustFromResolver[*Registry](serviceContainer, ServiceOpenApiRegistry)
}

func RegistryMustFromResolver(resolver containercontract.Resolver) *Registry {
    return container.MustFromResolver[*Registry](resolver, ServiceOpenApiRegistry)
}

/* InfoFromResolver returns the document Info registered by the application, or an empty Info when none was registered (the Info is optional metadata, not a hard dependency). */
func InfoFromResolver(resolver containercontract.Resolver) Info {
    if false == resolver.Has(ServiceOpenApiInfo) {
        return Info{}
    }

    return container.MustFromResolver[Info](resolver, ServiceOpenApiInfo)
}
