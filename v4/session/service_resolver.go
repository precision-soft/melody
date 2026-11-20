package session

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const (
    ServiceSessionManager = "service.session.manager"
    ServiceSessionStorage = "service.session.storage"
)

func SessionMustFromRuntime(runtimeInstance containercontract.Runtime) sessioncontract.Manager {
    return runtime.MustFromRuntime[sessioncontract.Manager](runtimeInstance, ServiceSessionManager)
}

func SessionMustFromResolver(ctx context.Context, resolver containercontract.Resolver) sessioncontract.Manager {
    return container.MustFromResolver[sessioncontract.Manager](ctx, resolver, ServiceSessionManager)
}

func SessionStorageMustFromResolver(ctx context.Context, resolver containercontract.Resolver) sessioncontract.Storage {
    return container.MustFromResolver[sessioncontract.Storage](ctx, resolver, ServiceSessionStorage)
}
