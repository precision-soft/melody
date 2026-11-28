package awss3

import (
    "errors"
    "fmt"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* NewClient builds the minio client of endpoint, host:port without a scheme, an empty one refused naming the argument, under config's credentials. */
func NewClient(endpoint string, config Config) (*minio.Client, error) {
    if "" == endpoint {
        return nil, exception.NewError("object storage endpoint is empty", exceptioncontract.Context{"argument": "endpoint"}, nil)
    }

    /* minio treats an empty access key OR an empty secret key as a request for ANONYMOUS access rather than an error, so a missing credential env var would boot cleanly and serve reads off any public bucket policy while writes fail much later with a bare AccessDenied. A caller who wants anonymous access holds a decision, not an accident, and can build the minio client directly. */
    if "" == config.AccessKey || "" == config.SecretKey {
        return nil, exception.NewError(
            "object storage credentials are incomplete: an empty access key or secret key would silently downgrade the client to anonymous access",
            map[string]any{"field": incompleteCredentialField(config), "endpoint": endpoint, "accessKeySet": "" != config.AccessKey, "secretKeySet": "" != config.SecretKey},
            nil,
        )
    }

    client, clientErr := minio.New(endpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
        Secure: false == config.Insecure,
        Region: config.Region,
    })
    if nil != clientErr {
        return nil, exception.NewError(
            "object storage client creation failed",
            map[string]any{"endpoint": endpoint},
            clientErr,
        )
    }

    return client, nil
}

func incompleteCredentialField(config Config) string {
    if "" == config.AccessKey {
        return "Config.AccessKey"
    }

    return "Config.SecretKey"
}

/* Config declares the client's credentials and transport: AccessKey and SecretKey are required; Region is the client's default region, the server's when empty. */
type Config struct {
    AccessKey string
    SecretKey string
    /* Insecure selects plaintext http; the zero value is https, so a configuration that names nothing crosses the network encrypted. Over plaintext, credentials and objects cross the network in clear and every presigned url is http: set it only for localhost or a loopback address, since a plaintext endpoint off this machine is named once per process, at WARNING, as the boot warning aws.s3.plaintextEndpoint. */
    Insecure bool
    Region   string
}

/* String and Format keep AccessKey and SecretKey out of every rendering fmt routes through this value's methods, each secret reduced to whether it was set; Format answers %#v and the numeric verbs too, and value receivers make a Config and a pointer to it redact alike. %p and %w never reach Format, and a Config held in an unexported field of another struct is walked by reflection with no method called. */
func (instance Config) String() string {
    return fmt.Sprintf(
        "aws.s3.Config{Region:%q, Insecure:%v, AccessKey:[redacted set=%v], SecretKey:[redacted set=%v]}",
        instance.Region,
        instance.Insecure,
        "" != instance.AccessKey,
        "" != instance.SecretKey,
    )
}

func (instance Config) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(instance.String()))
}

/* EnsureBucketOptions declares how a bucket is created when it does not exist: Region, the client's when empty, us-east-1 when the client names none either. */
type EnsureBucketOptions struct {
    Region string
}

/* EnsureBucket creates the bucket when it does not exist, its calls bounded by the runtime's context; one that exists, or that a concurrent replica created first, is answered as success. A nil runtime is refused. */
func EnsureBucket(runtimeInstance containercontract.Runtime, client *minio.Client, bucket string, options EnsureBucketOptions) error {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance, "awss3.EnsureBucket"); nil != runtimeErr {
        return runtimeErr
    }

    ctx := runtimeInstance.Context()

    exists, existsErr := client.BucketExists(ctx, bucket)
    if nil != existsErr {
        return exception.NewError("object storage bucket check failed", map[string]any{"bucket": bucket}, existsErr)
    }

    if true == exists {
        return nil
    }

    makeErr := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: options.Region})
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

        /* a re-check that could not answer leaves the question open rather than answered "taken by another account": the refusal carries both, so an operator reads a network failure as one instead of chasing a name clash */
        if nil != recheckErr {
            return exception.NewError("object storage bucket creation failed and the re-check of its existence could not complete", map[string]any{"bucket": bucket}, errors.Join(makeErr, recheckErr))
        }
    }

    return exception.NewError("object storage bucket creation failed", map[string]any{"bucket": bucket}, makeErr)
}
