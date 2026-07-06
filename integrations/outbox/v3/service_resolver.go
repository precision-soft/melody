package outbox

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

const ServiceStore = "service.outbox.store"

const ServiceRelay = "service.outbox.relay"

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterStoreService(registrar ServiceRegistrar, store *Store) {
    registrar.RegisterService(
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            return store, nil
        },
    )
}

func StoreMustFromResolver(resolver containercontract.Resolver) *Store {
    return container.MustFromResolver[*Store](resolver, ServiceStore)
}

func StoreMustFromContainer(serviceContainer containercontract.Container) *Store {
    return container.MustFromResolver[*Store](serviceContainer, ServiceStore)
}

func RegisterRelayService(registrar ServiceRegistrar, relay *Relay) {
    registrar.RegisterService(
        ServiceRelay,
        func(resolver containercontract.Resolver) (*Relay, error) {
            return relay, nil
        },
    )
}

func RelayMustFromResolver(resolver containercontract.Resolver) *Relay {
    return container.MustFromResolver[*Relay](resolver, ServiceRelay)
}

func RelayMustFromContainer(serviceContainer containercontract.Container) *Relay {
    return container.MustFromResolver[*Relay](serviceContainer, ServiceRelay)
}
