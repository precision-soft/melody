package outbox

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

const ServiceStore = "service.outbox.store"

const ServiceRelay = "service.outbox.relay"

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

func RegisterStoreService(registrar ServiceRegistrar, store *Store) {
    registrar.RegisterService(
        ServiceStore,
        func(resolver containercontract.Resolver) (*Store, error) {
            return store, nil
        },
        container.RegisterOptions{},
    )
}

func StoreMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Store {
    return container.MustFromResolver[*Store](ctx, resolver, ServiceStore)
}

func StoreMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) *Store {
    return container.MustFromResolver[*Store](ctx, serviceContainer, ServiceStore)
}

func RegisterRelayService(registrar ServiceRegistrar, relay *Relay) {
    registrar.RegisterService(
        ServiceRelay,
        func(resolver containercontract.Resolver) (*Relay, error) {
            return relay, nil
        },
        container.RegisterOptions{},
    )
}

func RelayMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Relay {
    return container.MustFromResolver[*Relay](ctx, resolver, ServiceRelay)
}

func RelayMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) *Relay {
    return container.MustFromResolver[*Relay](ctx, serviceContainer, ServiceRelay)
}
