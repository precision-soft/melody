package cache

import (
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycache "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type serviceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* registerBackendService resolves the configuration at the call, so a limit outside its rule fails the boot rather than the first resolve; the backend is built over the client of the resolved connection, and the declared edge refuses at boot an application that registers no connection. Each round trip is bounded by BackendConfig.CallTimeout, 1s when left out, on top of the runtime's context. */
func registerBackendService(registrar serviceRegistrar, config BackendConfig) {
    resolved, resolveErr := resolveBackendConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            /* the backend borrows the client and does not close it; resolving the owning connection records the edge that closes the client after this backend at teardown */
            connection, connectionErr := container.FromResolver[*melodyrueidis.Connection](resolver.Runtime().Context(), resolver, melodyrueidis.ServiceConnection)
            if nil != connectionErr {
                return nil, connectionErr
            }

            return &BackendService{
                backend: newBackendFromResolved(connection.Client(), resolved),
            }, nil
        },
        container.RegisterOptions{TeardownDependencyNames: []string{melodyrueidis.ServiceConnection}},
    )
}
