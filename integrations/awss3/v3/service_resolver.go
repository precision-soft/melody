package awss3

import (
    "github.com/minio/minio-go/v7"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

func RegisterStorageService(registrar ServiceRegistrar, client *minio.Client, bucket string) {
    registrar.RegisterService(
        melodystorage.ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            return NewStorage(client, bucket), nil
        },
    )
}
