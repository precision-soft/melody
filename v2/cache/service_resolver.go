package cache

import (
    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

const (
    ServiceCache           = "service.cache"
    ServiceCacheBackend    = "service.cache.backend"
    ServiceCacheSerializer = "service.cache.serializer"
)

func CacheMustFromContainer(serviceContainer containercontract.Container) cachecontract.Cache {
    return CacheMustFromResolver(serviceContainer)
}

func CacheMustFromResolver(resolver containercontract.Resolver) cachecontract.Cache {
    return container.MustFromResolver[cachecontract.Cache](resolver, ServiceCache)
}

func CacheBackendMustFromContainer(serviceContainer containercontract.Container) cachecontract.Backend {
    return container.MustFromResolver[cachecontract.Backend](serviceContainer, ServiceCacheBackend)
}

func CacheSerializerMustFromContainer(serviceContainer containercontract.Container) cachecontract.Serializer {
    return container.MustFromResolver[cachecontract.Serializer](serviceContainer, ServiceCacheSerializer)
}

func CacheBackendMustFromResolver(resolver containercontract.Resolver) cachecontract.Backend {
    return container.MustFromResolver[cachecontract.Backend](resolver, ServiceCacheBackend)
}

func CacheSerializerMustFromResolver(resolver containercontract.Resolver) cachecontract.Serializer {
    return container.MustFromResolver[cachecontract.Serializer](resolver, ServiceCacheSerializer)
}
