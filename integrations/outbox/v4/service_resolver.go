package outbox

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

const ServiceStore = "service.outbox.store"

const ServiceRelay = "service.outbox.relay"

func StoreMustFromRuntime(runtimeInstance containercontract.Runtime) *Store {
    return runtime.MustFromRuntime[*Store](runtimeInstance, ServiceStore)
}

func StoreMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Store {
    return container.MustFromResolver[*Store](ctx, resolver, ServiceStore)
}

func RelayMustFromResolver(ctx context.Context, resolver containercontract.Resolver) *Relay {
    return container.MustFromResolver[*Relay](ctx, resolver, ServiceRelay)
}

