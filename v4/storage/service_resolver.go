package storage

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

const ServiceStorage = "service.storage.storage"

func StorageMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) storagecontract.Storage {
    return container.MustFromResolver[storagecontract.Storage](ctx, serviceContainer, ServiceStorage)
}

func StorageMustFromResolver(ctx context.Context, resolver containercontract.Resolver) storagecontract.Storage {
    return container.MustFromResolver[storagecontract.Storage](ctx, resolver, ServiceStorage)
}
