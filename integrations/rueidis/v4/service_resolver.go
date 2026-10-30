package rueidis

import (
    melodyclock "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodylock "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/redis/rueidis"
)

const (
    ServiceClient     = "service.rueidis.client"
    ServiceConnection = "service.rueidis.connection"
    ServiceTokenStore = "service.rueidis.token_store"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* RegisterConnectionService registers the Connection that owns the client, so the container's ordered teardown, which closes what answers Close() error and was resolved at least once, closes the client. Every client-backed provider in this package resolves the connection when it is registered, so a run that resolves a locker, a token store or the client closes the connection after them; a run that resolves none leaves it unclosed, as the messagebus transports do. */
func RegisterConnectionService(registrar ServiceRegistrar, connection *Connection) {
    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*Connection, error) {
            return connection, nil
        },
        container.RegisterOptions{},
    )
}

/* resolveConnectionEdge records the consumer-to-connection dependency edge when a connection service is registered: resolving it through the resolver building the consumer orders the teardown, consumers first and the connection after. Without a registered connection the edge is skipped. */
func resolveConnectionEdge(resolver containercontract.Resolver) {
    if true == resolver.Has(ServiceConnection) {
        container.MustFromResolver[*Connection](resolver, ServiceConnection)
    }
}

func RegisterClientService(registrar ServiceRegistrar, client rueidis.Client) {
    registrar.RegisterService(
        ServiceClient,
        func(resolver containercontract.Resolver) (rueidis.Client, error) {
            resolveConnectionEdge(resolver)

            return client, nil
        },
        container.RegisterOptions{},
    )
}

func ClientMustFromResolver(resolver containercontract.Resolver) rueidis.Client {
    return container.MustFromResolver[rueidis.Client](resolver, ServiceClient)
}

func ClientMustFromContainer(serviceContainer containercontract.Container) rueidis.Client {
    return container.MustFromResolver[rueidis.Client](serviceContainer, ServiceClient)
}

/* RegisterLockerService builds the locker at the call, so a configuration outside its rule fails the boot rather than the first resolve. */
func RegisterLockerService(registrar ServiceRegistrar, client rueidis.Client, config LockerConfig) {
    locker := NewLocker(client, config)

    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolveConnectionEdge(resolver)

            return locker, nil
        },
        container.RegisterOptions{},
    )
}

/* RegisterTokenStoreService resolves the configuration at the call, so a limit outside its rule fails the boot rather than the first resolve; the container's clock reaches a store whose config names none. */
func RegisterTokenStoreService(registrar ServiceRegistrar, client rueidis.Client, config TokenStoreConfig) {
    if nil == client {
        exception.Panic(exception.NewError("redis token store client is nil", nil, nil))
    }

    resolved, resolveErr := resolveTokenStoreConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    registrar.RegisterService(
        ServiceTokenStore,
        func(resolver containercontract.Resolver) (securitycontract.RevocableTokenStore, error) {
            resolveConnectionEdge(resolver)

            storeConfig := resolved
            if nil == storeConfig.clock {
                if clockInstance, clockErr := container.FromResolver[clockcontract.Clock](resolver, melodyclock.ServiceClock); nil == clockErr {
                    storeConfig.clock = clockInstance
                }
            }

            return newTokenStoreFromResolved(client, storeConfig), nil
        },
        container.RegisterOptions{},
    )
}

func TokenStoreMustFromResolver(resolver containercontract.Resolver) securitycontract.RevocableTokenStore {
    return container.MustFromResolver[securitycontract.RevocableTokenStore](resolver, ServiceTokenStore)
}

func TokenStoreMustFromContainer(serviceContainer containercontract.Container) securitycontract.RevocableTokenStore {
    return container.MustFromResolver[securitycontract.RevocableTokenStore](serviceContainer, ServiceTokenStore)
}

func EpochRevocableTokenStoreMustFromResolver(resolver containercontract.Resolver) securitycontract.EpochRevocableTokenStore {
    return container.MustFromResolver[securitycontract.EpochRevocableTokenStore](resolver, ServiceTokenStore)
}
