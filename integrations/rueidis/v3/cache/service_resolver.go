package cache

import (
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodycache "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/container"
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
            /* the backend borrows the client and declines to close it; resolving the owning Connection — when one is registered — records the dependency edge that closes the client AFTER this backend at teardown */
            if true == resolver.Has(melodyrueidis.ServiceConnection) {
                container.MustFromResolver[*melodyrueidis.Connection](resolver, melodyrueidis.ServiceConnection)
            }

            return NewBackendService(client, prefix, 0, 0)
        },
    )
}
