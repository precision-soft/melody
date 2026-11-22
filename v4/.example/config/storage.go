package config

import (
    melodyawss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
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

    client, clientErr := melodyawss3.NewClient(endpoint, melodyawss3.Config{
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
}

/* registerStorageServices registers the local-disk storage the example serves without S3; with S3 configured the aws.s3 module registers the storage and checks its bucket */
func (instance *Module) registerStorageServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    instance.registerStorageService(registrar)
}

/* storageModuleConfig hands the aws.s3 module the client, the bucket and the bucket check: the storage is built at boot and checks its bucket under the runtime of whoever resolves it, so a missing bucket or an unreachable S3 is a boot warning and the first upload checks it again */
func (instance *Module) storageModuleConfig() melodyawss3.ModuleConfig {
    return melodyawss3.ModuleConfig{
        Client: instance.storageClient,
        Bucket: instance.storageBucket,
        BucketCheck: melodyawss3.BucketCheckConfig{
            Enabled:     true,
            Region:      instance.parameterValue(parameterS3Region),
            BootFailure: melodycontainer.BootFailureWarns,
        },
    }
}
