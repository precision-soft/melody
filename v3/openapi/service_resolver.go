package openapi

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

const ServiceOpenApiRegistry = "service.openapi.registry"

func RegistryMustFromContainer(serviceContainer containercontract.Container) *Registry {
    return container.MustFromResolver[*Registry](serviceContainer, ServiceOpenApiRegistry)
}

func RegistryMustFromResolver(resolver containercontract.Resolver) *Registry {
    return container.MustFromResolver[*Registry](resolver, ServiceOpenApiRegistry)
}
