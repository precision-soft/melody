package config

import (
    "context"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *Module) buildStorage() {
    endpoint := instance.environmentValue(environmentKeyS3Endpoint)
    if "" == endpoint {
        return
    }

    bucket := instance.environmentValue(environmentKeyS3Bucket)
    if "" == bucket {
        bucket = "melody-example"
    }

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{
        Endpoint:  endpoint,
        AccessKey: instance.environmentValue(environmentKeyS3AccessKey),
        SecretKey: instance.environmentValue(environmentKeyS3SecretKey),
        Secure:    "true" == instance.environmentValue(environmentKeyS3Secure),
        Region:    instance.environmentValue(environmentKeyS3Region),
    })
    if nil != clientErr {
        exception.Panic(exception.FromError(clientErr))
    }

    if ensureErr := melodyawss3.EnsureBucket(context.Background(), client, bucket, instance.environmentValue(environmentKeyS3Region)); nil != ensureErr {
        exception.Panic(exception.FromError(ensureErr))
    }

    instance.storageClient = client
    instance.storageBucket = bucket
    instance.storage = melodyawss3.NewStorage(client, bucket)
}
