package storage

import (
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

const ServiceStorage = "service.storage.storage"

func StorageMustFromContainer(serviceContainer containercontract.Container) storagecontract.Storage {
    return container.MustFromResolver[storagecontract.Storage](serviceContainer, ServiceStorage)
}

func StorageMustFromResolver(resolver containercontract.Resolver) storagecontract.Storage {
    return container.MustFromResolver[storagecontract.Storage](resolver, ServiceStorage)
}
