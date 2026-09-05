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

    /* minio treats an empty access key OR an empty secret key as a request for ANONYMOUS access rather than an error, so a missing credential env var would boot cleanly and serve reads off any public bucket policy while writes fail much later with a bare AccessDenied. A caller who wants anonymous access holds a decision, not an accident, and can build the minio client directly. */
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

/* String and Format keep AccessKey and SecretKey out of every rendering fmt can reach. Both are EXPORTED fields, so a plain %v walks them and prints the object-storage credentials in the clear. Format is what makes this complete: fmt consults a Formatter before Stringer and GoStringer and for every verb, so Format answers %#v and the numeric verbs (%d, %o, …) that would otherwise reflection-walk the struct, and it delegates to String for the one rendering. A separate GoString would be dead — Format shadows it. The receivers are values so that both a Config and a pointer to it redact, and each secret is reduced to whether it was set. This mirrors the redaction the encrypt key provider carries for the same reason. */
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

    /* the exists-then-create pair is not atomic: two replicas booting together both read "absent" and race the creation, and the loser's refusal names a bucket that now exists — exactly the state this function promises. The re-check, not the error code alone, settles it: BucketAlreadyExists on AWS also means the name is taken by ANOTHER account, and only a fresh BucketExists answering true proves the bucket is this caller's to use. */
    switch minio.ToErrorResponse(makeErr).Code {
    case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
        existsNow, recheckErr := client.BucketExists(ctx, bucket)
        if nil == recheckErr && true == existsNow {
            return nil
        }
    }

    return exception.NewError("object storage bucket creation failed", map[string]any{"bucket": bucket}, makeErr)
}
