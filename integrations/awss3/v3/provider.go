package awss3

import (
    "context"
    "fmt"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/precision-soft/melody/v3/exception"
)

func NewClient(config Config) (*minio.Client, error) {
    if "" == config.Endpoint {
        return nil, exception.NewError("object storage endpoint is empty", nil, nil)
    }

    if "" == config.AccessKey || "" == config.SecretKey {
        return nil, exception.NewError(
            "object storage credentials are incomplete: an empty access key or secret key would silently downgrade the client to anonymous access",
            map[string]any{"endpoint": config.Endpoint, "accessKeySet": "" != config.AccessKey, "secretKeySet": "" != config.SecretKey},
            nil,
        )
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

/* String redacts AccessKey and SecretKey; Format uses the same representation when fmt invokes the Formatter interface. Both values and pointers implement it. Explicit field access, conversions and formatting paths that bypass Formatter do not provide redaction. */
func (instance Config) String() string {
    return fmt.Sprintf(
        "awss3.Config{Endpoint:%q, Region:%q, Secure:%v, AccessKey:[redacted set=%v], SecretKey:[redacted set=%v]}",
        instance.Endpoint,
        instance.Region,
        instance.Secure,
        "" != instance.AccessKey,
        "" != instance.SecretKey,
    )
}

func (instance Config) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(instance.String()))
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
    if nil == makeErr {
        return nil
    }

    switch minio.ToErrorResponse(makeErr).Code {
    case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
        existsNow, recheckErr := client.BucketExists(ctx, bucket)
        if nil == recheckErr && true == existsNow {
            return nil
        }
    }

    return exception.NewError("object storage bucket creation failed", map[string]any{"bucket": bucket}, makeErr)
}
