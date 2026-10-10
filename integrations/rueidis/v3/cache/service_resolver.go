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

/* RegisterBackendService registers the backend with no options, so every operation dispatched without a caller context is unbounded. Register through RegisterBackendServiceWithOptions with WithCommandTimeout to bound the request-path reads: the client retries a read-only command for as long as a context without deadline allows, so a store that stops answering holds a Get. */
func RegisterBackendService(registrar ServiceRegistrar, client rueidis.Client, prefix string) {
    RegisterBackendServiceWithOptions(registrar, client, prefix)
}

/* RegisterBackendServiceWithOptions registers the backend built with the given options, the way NewBackendService takes them; scan count and delete batch stay at their defaults. */
func RegisterBackendServiceWithOptions(registrar ServiceRegistrar, client rueidis.Client, prefix string, options ...BackendOption) {
    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            /* the backend borrows the client and does not close it; resolving the owning Connection, when one is registered, records the dependency edge that closes the client after this backend at teardown */
            if true == resolver.Has(melodyrueidis.ServiceConnection) {
                container.MustFromResolver[*melodyrueidis.Connection](resolver, melodyrueidis.ServiceConnection)
            }

            return NewBackendService(client, prefix, 0, 0, options...)
        },
    )
}
