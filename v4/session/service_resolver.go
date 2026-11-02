package session

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const (
    ServiceSessionManager = "service.session.manager"
    ServiceSessionStorage = "service.session.storage"
)

func SessionMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) sessioncontract.Manager {
    return container.MustFromResolver[sessioncontract.Manager](ctx, serviceContainer, ServiceSessionManager)
}

func SessionStorageMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) sessioncontract.Storage {
    return container.MustFromResolver[sessioncontract.Storage](ctx, serviceContainer, ServiceSessionStorage)
}

func SessionStorageMustFromResolver(ctx context.Context, resolver containercontract.Resolver) sessioncontract.Storage {
    return container.MustFromResolver[sessioncontract.Storage](ctx, resolver, ServiceSessionStorage)
}
