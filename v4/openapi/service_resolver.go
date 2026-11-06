package openapi

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

const ServiceOpenApiRegistry = "service.openapi.registry"

const ServiceOpenApiInfo = "service.openapi.info"

func RegistryMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) *Registry {
    return container.MustFromResolver[*Registry](ctx, serviceContainer, ServiceOpenApiRegistry)
}

func RegistryMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Registry {
    return container.MustFromResolver[*Registry](ctx, resolver, ServiceOpenApiRegistry)
}

/* InfoFromResolver returns the document Info registered by the application, or an empty Info when none was registered (the Info is optional metadata, not a hard dependency). */
func InfoFromResolver(ctx context.Context, resolver containercontract.Resolver) Info {
    if false == resolver.Has(ServiceOpenApiInfo) {
        return Info{}
    }

    return container.MustFromResolver[Info](ctx, resolver, ServiceOpenApiInfo)
}
