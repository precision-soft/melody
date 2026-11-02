package cache

import (
    "context"

    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

const (
    ServiceCache           = "service.cache"
    ServiceCacheBackend    = "service.cache.backend"
    ServiceCacheSerializer = "service.cache.serializer"
)

func CacheMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) cachecontract.Cache {
    return CacheMustFromResolver(ctx, serviceContainer)
}

func CacheMustFromResolver(ctx context.Context, resolver containercontract.Resolver) cachecontract.Cache {
    return container.MustFromResolver[cachecontract.Cache](ctx, resolver, ServiceCache)
}

func CacheBackendMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) cachecontract.Backend {
    return container.MustFromResolver[cachecontract.Backend](ctx, serviceContainer, ServiceCacheBackend)
}

func CacheSerializerMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) cachecontract.Serializer {
    return container.MustFromResolver[cachecontract.Serializer](ctx, serviceContainer, ServiceCacheSerializer)
}

func CacheBackendMustFromResolver(ctx context.Context, resolver containercontract.Resolver) cachecontract.Backend {
    return container.MustFromResolver[cachecontract.Backend](ctx, resolver, ServiceCacheBackend)
}

func CacheSerializerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) cachecontract.Serializer {
    return container.MustFromResolver[cachecontract.Serializer](ctx, resolver, ServiceCacheSerializer)
}
