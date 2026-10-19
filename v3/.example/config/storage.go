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
        Secure:    objectStorageIsSecure(instance.environmentValue(environmentKeyS3Insecure)),
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

/* objectStorageIsSecure dials https unless S3_INSECURE is exactly "true", the opt-out the sql providers read too: an omitted key never downgrades the transport */
func objectStorageIsSecure(insecureValue string) bool {
    return "true" != insecureValue
}
