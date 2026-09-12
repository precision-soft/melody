package awss3

import (
    "github.com/minio/minio-go/v7"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodystorage "github.com/precision-soft/melody/v3/storage"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

type ServiceRegistrar interface {
    RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption)
}

/* RegisterStorageService validates its arguments at registration rather than deferring to the provider closure: NewStorage panics on a nil client or an empty bucket, and inside the closure that panic would fire at the FIRST RESOLUTION — possibly on a request path — instead of at boot, where every other integration reports an unusable configuration. */
func RegisterStorageService(registrar ServiceRegistrar, client *minio.Client, bucket string) {
    if nil == client {
        exception.Panic(exception.NewError("object storage client is nil", nil, nil))
    }

    if "" == bucket {
        exception.Panic(exception.NewError("object storage bucket is empty", nil, nil))
    }

    registrar.RegisterService(
        melodystorage.ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            return NewStorage(client, bucket), nil
        },
    )
}
