package cache

import (
    "context"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycache "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/redis/rueidis"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* RegisterBackendService resolves the configuration at the call, so a nil client or a limit outside its rule fails the boot rather than the first resolve. The context-less operations are bounded by BackendConfig.CommandTimeout, 1s when left out. */
func RegisterBackendService(registrar ServiceRegistrar, client rueidis.Client, config BackendConfig) {
    if nil == client {
        exception.Panic(exception.NewError("redis client is nil", nil, nil))
    }

    resolved, resolveErr := resolveBackendConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            /* the backend borrows the client and does not close it; resolving the owning Connection, when one is registered, records the dependency edge that closes the client after this backend at teardown */
            if true == resolver.Has(melodyrueidis.ServiceConnection) {
                container.MustFromResolver[*melodyrueidis.Connection](container.ContextOf(resolver), resolver, melodyrueidis.ServiceConnection)
            }

            return &BackendService{
                client:  client,
                backend: newBackendFromResolved(context.Background(), client, resolved),
            }, nil
        },
        container.RegisterOptions{},
    )
}
