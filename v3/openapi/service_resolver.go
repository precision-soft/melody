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

/* InfoFromResolver returns optional document metadata, using empty Info when no registration exists. */
func InfoFromResolver(resolver containercontract.Resolver) Info {
    if false == resolver.Has(ServiceOpenApiInfo) {
        return Info{}
    }

    return container.MustFromResolver[Info](resolver, ServiceOpenApiInfo)
}
