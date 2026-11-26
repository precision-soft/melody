package rueidis

import (
    "context"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/redis/rueidis"
)

const (
    ServiceConnection = "service.rueidis.connection"
    ServiceTokenStore = "service.rueidis.token_store"
)

type serviceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* registerConnectionService registers the factory of the Connection that owns the client, so the container's ordered teardown, which closes what answers CloseWithContext and was resolved at least once, closes the client. Every client-backed provider in this package resolves the connection, so the teardown closes the connection after them. */
func registerConnectionService(
    registrar serviceRegistrar,
    connection func(resolver containercontract.Resolver) (*Connection, error),
    options containercontract.RegisterOptions,
) {
    registrar.RegisterService(ServiceConnection, connection, options)
}

func resolveConnection(resolver containercontract.Resolver) (*Connection, error) {
    return container.FromResolver[*Connection](resolver.Runtime().Context(), resolver, ServiceConnection)
}

/* ClientMustFromRuntime answers the client of the connection service resolved through the runtime's scope; the connection keeps its lifecycle. */
func ClientMustFromRuntime(runtimeInstance containercontract.Runtime) rueidis.Client {
    return runtime.MustFromRuntime[*Connection](runtimeInstance, ServiceConnection).Client()
}

func ClientMustFromResolver(ctx context.Context, resolver containercontract.Resolver) rueidis.Client {
    return container.MustFromResolver[*Connection](ctx, resolver, ServiceConnection).Client()
}

/* registerLockerService resolves the configuration at the call, so a configuration outside its rule fails the boot rather than the first resolve; the locker is built over the resolved connection. */
func registerLockerService(registrar serviceRegistrar, config LockerConfig) {
    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, lockerCallTimeoutRule)
    if nil != callTimeoutErr {
        exception.Panic(exception.FromError(callTimeoutErr))
    }

    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            connection, connectionErr := resolveConnection(resolver)
            if nil != connectionErr {
                return nil, connectionErr
            }

            return &Locker{client: connection.Client(), callTimeout: callTimeout.Value}, nil
        },
        container.RegisterOptions{},
    )
}

/* registerTokenStoreService resolves the configuration at the call, so a limit outside its rule fails the boot rather than the first resolve; the container's clock reaches a store whose config names none. */
func registerTokenStoreService(registrar serviceRegistrar, config TokenStoreConfig) {
    resolved, resolveErr := resolveTokenStoreConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    registrar.RegisterService(
        ServiceTokenStore,
        func(resolver containercontract.Resolver) (securitycontract.RevocableTokenStore, error) {
            connection, connectionErr := resolveConnection(resolver)
            if nil != connectionErr {
                return nil, connectionErr
            }

            storeConfig := resolved
            if nil == storeConfig.clock {
                if clockInstance, clockErr := container.FromResolver[clockcontract.Clock](resolver.Runtime().Context(), resolver, melodyclock.ServiceClock); nil == clockErr {
                    storeConfig.clock = clockInstance
                }
            }

            return newTokenStoreFromResolved(connection.Client(), storeConfig), nil
        },
        container.RegisterOptions{},
    )
}

func TokenStoreMustFromResolver(ctx context.Context, resolver containercontract.Resolver) securitycontract.RevocableTokenStore {
    return container.MustFromResolver[securitycontract.RevocableTokenStore](ctx, resolver, ServiceTokenStore)
}

func EpochRevocableTokenStoreMustFromResolver(ctx context.Context, resolver containercontract.Resolver) securitycontract.EpochRevocableTokenStore {
    return container.MustFromResolver[securitycontract.EpochRevocableTokenStore](ctx, resolver, ServiceTokenStore)
}
