# Melody AWS S3 integration (v4)

An S3-compatible implementation of the Melody core [`storage`](https://github.com/precision-soft/melody) contract, backed by [`minio-go`](https://github.com/minio/minio-go). Works with MinIO and AWS S3 (and any S3-compatible service).

It implements `storage/contract.Storage`, so application code written against the core abstraction can use local disk in development and object storage in production without changes.

## Version lines

This integration ships for the third and the fourth major (`github.com/precision-soft/melody/integrations/aws/s3/v4`); no v1 or v2 bindings are currently planned.

## Installation

```bash
go get github.com/precision-soft/melody/integrations/aws/s3/v4
```

```go
import awss3 "github.com/precision-soft/melody/integrations/aws/s3/v4"
```

## Usage

```go
client, clientErr := awss3.NewClient(awss3.Config{
    Endpoint:  "s3.example.com",
    AccessKey: "...",
    SecretKey: "...",
})
if nil != clientErr {
    return clientErr
}

if ensureErr := awss3.EnsureBucket(ctx, client, "documents", awss3.EnsureBucketConfig{}); nil != ensureErr {
    return ensureErr
}

store := awss3.NewStorage(client, "documents", awss3.StorageConfig{})

putErr := store.Put(runtimeInstance, "labels/awb-123.pdf", reader, size, storagecontract.PutOptions{
    ContentType: "application/pdf",
})

url, _ := store.PresignedUrl(runtimeInstance, "labels/awb-123.pdf", 15*time.Minute)
```

### Transport security

The zero `Config` dials **https**. `Config.Insecure` selects plain http: credentials and objects cross the network in clear, and every presigned url is `http://`, so set it only for `localhost` or a loopback address. A storage over a plain http endpoint anywhere else — a private address and a single-label host such as a compose service name included — names it once, at WARNING, at the first use of one of its doors, with `bootWarning=awss3.plaintextEndpoint` and the endpoint's host in the record, never a credential. The endpoint is read from the client itself, so a client built without `NewClient` is judged too.

`EnsureBucketConfig.Region` is the region a missing bucket is created in; left empty, minio creates it in the client's region, and in `us-east-1` when the client names none either.

### Plug-and-play registration

Register the S3 backend under the core `storage.ServiceStorage` service name in one call, so handlers resolve it from the container with `storage.StorageMustFromResolver`:

```go
awss3.RegisterStorageService(registrar, client, "documents", awss3.StorageConfig{})
```

Or bundle it as a self-registering application module — one `RegisterModule` call registers the storage service (an empty `ModuleConfig{}` registers nothing; a bucket or a storage configuration with no client, or a configured client with an empty bucket or a limit outside its rule is refused at boot):

```go
app.RegisterModule(awss3.NewModule(awss3.ModuleConfig{Client: client, Bucket: "documents"}))
```

`StorageConfig` declares the storage's two bounds, each its default when left out and refused at `NewStorage`, at the `RegisterStorageService` call and through the module, naming the field, when below its minimum or unlimited:

| Field                   | Meaning                                                                                                        | Default  |
|-------------------------|----------------------------------------------------------------------------------------------------------------|----------|
| `PutSpoolLimit`         | what a `Put` may hold in memory: a non-seekable body declared at or below it is buffered, a larger one streams | `16 MiB` |
| `MultipartAbortTimeout` | the bound of the detached abort of the multipart upload a cancelled `Put` leaves behind                        | `10s`    |

## Footguns & caveats

- `NewClient` refuses an empty `AccessKey` or `SecretKey`: minio reads either empty credential as a request for **anonymous** access rather than an error, so a missing env var would boot cleanly and fail much later with a bare `AccessDenied`. Build the minio client directly if anonymous access is genuinely what you want.
- `Config` redacts `AccessKey` and `SecretKey` in every `fmt` rendering (`String`/`Format`): a config dropped into a log or an error context shows each secret only as set-or-not, never the value.
- `EnsureBucket` treats a lost creation race as success once a re-check confirms the bucket is usable — two replicas booting together no longer fail the loser — while a name owned by another account keeps failing.
- Neither `*minio.Client` nor `Storage` expose a `Close`: minio-go offers none, so the client's pooled connections live until idle timeout or process exit. This is a property of the underlying SDK, not a forgotten teardown door.
- `Put` enforces the size you declare. A seekable body (`*bytes.Reader`, `*strings.Reader`, `multipart.File`, `*os.File`) is measured before the upload; a non-seekable one (an `http.Request.Body`) is checked as it streams and the upload is cut off before its last byte if the body turns out to be longer, so a wrong size never leaves a truncated object at the key. Memory does not scale with the body: a declared size at or below `StorageConfig.PutSpoolLimit`, 16 MiB by default, is buffered so it can be rejected before any request, anything larger streams and is cut off mid-upload. Whether the upload goes up as one request or multipart is MinIO's decision by its own 16 MiB part size, whatever the limit. Pass `-1` when the size is unknown and the client streams the object with no size check.
- `Get` returns the object's reader after a `Stat`, so a missing object fails fast instead of erroring only on first read. Close the reader.
- `PresignedUrl` issues a presigned GET URL valid for the given expiry.
- A key is folded the way `LocalStorage` folds it (backslash to slash, `.` and empty segments and a leading slash folded away), and a key that carries a `..` segment is refused by name at every door before the bucket is asked, so a key that climbs out of one prefix never addresses an object under another.
- The integration test (`storage_test.go`) is skipped unless `MINIO_ENDPOINT` (and `MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY`) are set; it was verified against MinIO and LocalStack (the dev `docker-compose.yml` ships a LocalStack `s3` service).
