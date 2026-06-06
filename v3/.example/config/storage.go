package config

import (
    "context"
    "os"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *Module) buildStorage() {
    endpoint := os.Getenv("S3_ENDPOINT")
    if "" == endpoint {
        return
    }

    bucket := os.Getenv("S3_BUCKET")
    if "" == bucket {
        bucket = "melody-example"
    }

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{
        Endpoint:  endpoint,
        AccessKey: os.Getenv("S3_ACCESS_KEY"),
        SecretKey: os.Getenv("S3_SECRET_KEY"),
        Secure:    "true" == os.Getenv("S3_SECURE"),
        Region:    os.Getenv("S3_REGION"),
    })
    if nil != clientErr {
        exception.Panic(exception.FromError(clientErr))
    }

    if ensureErr := melodyawss3.EnsureBucket(context.Background(), client, bucket, os.Getenv("S3_REGION")); nil != ensureErr {
        exception.Panic(exception.FromError(ensureErr))
    }

    instance.storageClient = client
    instance.storageBucket = bucket
}
