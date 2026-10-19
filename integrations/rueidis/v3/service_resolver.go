package rueidis

import (
    melodyclock "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const (
    ServiceClient     = "service.rueidis.client"
    ServiceConnection = "service.rueidis.connection"
    ServiceTokenStore = "service.rueidis.token_store"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

/* RegisterConnectionService registers the Connection that owns the client, so the container's ordered teardown, which closes what answers Close() error and was resolved at least once, closes the client. Every client-backed provider in this package resolves the connection when it is registered, so a run that resolves a locker, a token store or the client closes the connection after them; a run that resolves none leaves it unclosed, as the messagebus transports do. */
func RegisterConnectionService(registrar ServiceRegistrar, connection *Connection) {
    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*Connection, error) {
            return connection, nil
        },
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
    )
}

func ClientMustFromResolver(resolver containercontract.Resolver) rueidis.Client {
    return container.MustFromResolver[rueidis.Client](resolver, ServiceClient)
}

func ClientMustFromContainer(serviceContainer containercontract.Container) rueidis.Client {
    return container.MustFromResolver[rueidis.Client](serviceContainer, ServiceClient)
}

func RegisterLockerService(registrar ServiceRegistrar, client rueidis.Client) {
    RegisterLockerServiceWithOptions(registrar, client)
}

/* RegisterLockerServiceWithOptions is RegisterLockerService with the options the locker is built with — WithLockerCallTimeout above all; the option-less door registers the locker at its defaults, and ModuleConfig.LockerOptions hands the same options through the module. */
func RegisterLockerServiceWithOptions(registrar ServiceRegistrar, client rueidis.Client, options ...LockerOption) {
    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            resolveConnectionEdge(resolver)

            return NewLockerWithOptions(client, options...), nil
        },
    )
}

func RegisterTokenStoreService(registrar ServiceRegistrar, client rueidis.Client, options ...TokenStoreOption) {
    registrar.RegisterService(
        ServiceTokenStore,
        func(resolver containercontract.Resolver) (securitycontract.RevocableTokenStore, error) {
            resolveConnectionEdge(resolver)

            resolvedOptions := make([]TokenStoreOption, 0, len(options)+1)

            if clockInstance, clockErr := container.FromResolver[clockcontract.Clock](resolver, melodyclock.ServiceClock); nil == clockErr {
                resolvedOptions = append(resolvedOptions, WithTokenStoreClock(clockInstance))
            }

            resolvedOptions = append(resolvedOptions, options...)

            return NewTokenStore(client, resolvedOptions...), nil
        },
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
