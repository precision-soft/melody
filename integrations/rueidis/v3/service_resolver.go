package rueidis

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/redis/rueidis"
)

const (
    ServiceClient     = "service.rueidis.client"
    ServiceTokenStore = "service.rueidis.token_store"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterClientService(registrar ServiceRegistrar, client rueidis.Client) {
    registrar.RegisterService(
        ServiceClient,
        func(resolver containercontract.Resolver) (rueidis.Client, error) {
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
    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver containercontract.Resolver) (lockcontract.Locker, error) {
            return NewLocker(client), nil
        },
    )
}

func RegisterTokenStoreService(registrar ServiceRegistrar, client rueidis.Client, options ...TokenStoreOption) {
    registrar.RegisterService(
        ServiceTokenStore,
        func(resolver containercontract.Resolver) (securitycontract.RevocableTokenStore, error) {
            return NewTokenStore(client, options...), nil
        },
    )
}

func TokenStoreMustFromResolver(resolver containercontract.Resolver) securitycontract.RevocableTokenStore {
    return container.MustFromResolver[securitycontract.RevocableTokenStore](resolver, ServiceTokenStore)
}

func TokenStoreMustFromContainer(serviceContainer containercontract.Container) securitycontract.RevocableTokenStore {
    return container.MustFromResolver[securitycontract.RevocableTokenStore](serviceContainer, ServiceTokenStore)
}
