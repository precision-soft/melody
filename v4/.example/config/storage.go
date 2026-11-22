package config

import (
    melodyawss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func (instance *Module) buildStorage() {
    endpoint := instance.parameterValue(parameterS3Endpoint)
    if "" == endpoint {
        return
    }

    bucket := instance.parameterValue(parameterS3Bucket)
    if "" == bucket {
        bucket = "melody-example"
    }

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{
        Endpoint:  endpoint,
        AccessKey: instance.parameterValue(parameterS3AccessKey),
        SecretKey: instance.parameterValue(parameterS3SecretKey),
        Insecure:  instance.booleanSwitch(environmentKeyS3Insecure, parameterS3Insecure),
        Region:    instance.parameterValue(parameterS3Region),
    })
    if nil != clientErr {
        exception.Panic(exception.FromError(clientErr))
    }

    instance.storageClient = client
    instance.storageBucket = bucket
    instance.storage = melodyawss3.NewStorage(client, bucket, melodyawss3.StorageConfig{})
}

/* ServiceStorageBucket is the example's bucket check, built at boot so a missing bucket fails Boot */
const ServiceStorageBucket = "service.example.storage_bucket"

type storageBucketReady struct{}

/* registerStorageServices registers the storage the example serves and, with S3 configured, its bucket check built at boot */
func (instance *Module) registerStorageServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    instance.registerStorageService(registrar)
    instance.registerStorageBucketService(registrar)
}

func (instance *Module) registerStorageBucketService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.storageClient {
        return
    }

    registrar.RegisterService(
        ServiceStorageBucket,
        func(resolver melodycontainercontract.Resolver) (*storageBucketReady, error) {
            if ensureErr := melodyawss3.EnsureBucket(resolver.Runtime(), instance.storageClient, instance.storageBucket, melodyawss3.EnsureBucketConfig{Region: instance.parameterValue(parameterS3Region)}); nil != ensureErr {
                return nil, ensureErr
            }

            return &storageBucketReady{}, nil
        },
        melodycontainer.RegisterOptions{BuiltAtBoot: true, BootFailure: melodycontainer.BootFailureFailsBoot},
    )
}
