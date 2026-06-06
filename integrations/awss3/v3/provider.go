package awss3

import (
    "context"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/precision-soft/melody/v3/exception"
)

func NewClient(config Config) (*minio.Client, error) {
    if "" == config.Endpoint {
        return nil, exception.NewError("object storage endpoint is empty", nil, nil)
    }

    client, clientErr := minio.New(config.Endpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
        Secure: config.Secure,
        Region: config.Region,
    })
    if nil != clientErr {
        return nil, exception.NewError(
            "object storage client creation failed",
            map[string]any{"endpoint": config.Endpoint},
            clientErr,
        )
    }

    return client, nil
}

type Config struct {
    Endpoint  string
    AccessKey string
    SecretKey string
    Secure    bool
    Region    string
}

func EnsureBucket(ctx context.Context, client *minio.Client, bucket string, region string) error {
    exists, existsErr := client.BucketExists(ctx, bucket)
    if nil != existsErr {
        return exception.NewError("object storage bucket check failed", map[string]any{"bucket": bucket}, existsErr)
    }

    if true == exists {
        return nil
    }

    makeErr := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
    if nil != makeErr {
        return exception.NewError("object storage bucket creation failed", map[string]any{"bucket": bucket}, makeErr)
    }

    return nil
}
