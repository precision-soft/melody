package event

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    ServiceEventDispatcher = "service.event.dispatcher"
)

func EventDispatcherMustFromRuntime(runtimeInstance containercontract.Runtime) eventcontract.EventDispatcher {
    return runtime.MustFromRuntime[eventcontract.EventDispatcher](runtimeInstance, ServiceEventDispatcher)
}

func EventDispatcherMustFromResolver(ctx context.Context, resolver containercontract.Resolver) eventcontract.EventDispatcher {
    return container.MustFromResolver[eventcontract.EventDispatcher](ctx, resolver, ServiceEventDispatcher)
}
