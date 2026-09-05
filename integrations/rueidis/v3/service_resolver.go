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

/* RegisterConnectionService registers the Connection that OWNS the client, so the container's ordered teardown — which closes what answers Close() error and only what was resolved at least once — can finally close the client nobody else will. Every client-backed provider in this package resolves the connection when it is registered, recording the dependency edge: whichever run resolves a locker, a token store or the client itself thereby guarantees the connection closes after them. A run that resolves none of them leaves the connection unresolved and therefore unclosed — the same guarantee shape the messagebus transports carry. */
func RegisterConnectionService(registrar ServiceRegistrar, connection *Connection) {
    registrar.RegisterService(
        ServiceConnection,
        func(resolver containercontract.Resolver) (*Connection, error) {
            return connection, nil
        },
    )
}

/* resolveConnectionEdge records the consumer→connection dependency edge when a connection service is registered: resolving it through the same resolver that is building the consumer is what orders the teardown — consumers close first, the connection after them. Without a registered connection there is nothing to order and the edge is skipped. */
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
