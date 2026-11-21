package outbox

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
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

func StoreMustFromRuntime(runtimeInstance containercontract.Runtime) *Store {
    return runtime.MustFromRuntime[*Store](runtimeInstance, ServiceStore)
}

func StoreMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Store {
    return container.MustFromResolver[*Store](ctx, resolver, ServiceStore)
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

