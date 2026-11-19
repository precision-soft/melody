package config

import (
    melodyawss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
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

    if ensureErr := melodyawss3.EnsureBucket(instance.processContext, client, bucket, melodyawss3.EnsureBucketConfig{Region: instance.parameterValue(parameterS3Region)}); nil != ensureErr {
        exception.Panic(exception.FromError(ensureErr))
    }

    instance.storageClient = client
    instance.storageBucket = bucket
    instance.storage = melodyawss3.NewStorage(client, bucket, melodyawss3.StorageConfig{})
}
