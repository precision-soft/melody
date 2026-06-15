package cache

import (
    melodycache "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/redis/rueidis"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterBackendService(registrar ServiceRegistrar, client rueidis.Client, prefix string) {
    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            return NewBackendService(client, prefix, 0, 0, 0)
        },
    )
}
