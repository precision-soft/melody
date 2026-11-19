package event

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

const (
    ServiceEventDispatcher = "service.event.dispatcher"
)

func EventDispatcherMustFromResolver(ctx context.Context, resolver containercontract.Resolver) eventcontract.EventDispatcher {
    return container.MustFromResolver[eventcontract.EventDispatcher](ctx, resolver, ServiceEventDispatcher)
}
