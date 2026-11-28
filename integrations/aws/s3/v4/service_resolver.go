package awss3

import (
    "github.com/minio/minio-go/v7"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

type serviceRegistrar interface {
    RegisterService(serviceName string, provider any, options containercontract.RegisterOptions)
}

/* registerStorageService validates its arguments and resolves the config at registration rather than deferring to the provider closure: NewStorage panics on a nil client, an empty bucket or a limit outside its rule, and inside the closure that panic would fire at the FIRST RESOLUTION — possibly on a request path — instead of at boot, where every other integration reports an unusable configuration. */
func registerStorageService(registrar serviceRegistrar, client *minio.Client, bucket string, config StorageConfig, bucketCheck BucketCheckConfig) {
    if nil == client {
        exception.Panic(exception.NewError("object storage client is nil", exceptioncontract.Context{"argument": "client"}, nil))
    }

    if "" == bucket {
        exception.Panic(exception.NewError("object storage bucket is empty", exceptioncontract.Context{"argument": "bucket"}, nil))
    }

    resolved, resolveErr := resolveStorageConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    registrar.RegisterService(
        melodystorage.ServiceStorage,
        func(resolver containercontract.Resolver) (storagecontract.Storage, error) {
            if true == bucketCheck.Enabled {
                if ensureErr := EnsureBucket(resolver.Runtime(), client, bucket, EnsureBucketOptions{Region: bucketCheck.Region}); nil != ensureErr {
                    return nil, ensureErr
                }
            }

            return newStorage(client, bucket, resolved), nil
        },
        containercontract.RegisterOptions{BuiltAtBoot: bucketCheck.Enabled, BootFailure: bucketCheck.BootFailure},
    )
}
