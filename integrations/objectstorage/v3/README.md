# Melody object storage integration (v3)

An S3-compatible implementation of the Melody core [`storage`](https://github.com/precision-soft/melody) contract, backed by [`minio-go`](https://github.com/minio/minio-go). Works with MinIO and AWS S3 (and any S3-compatible service).

It implements `storage/contract.Storage`, so application code written against the core abstraction can use local disk in development and object storage in production without changes.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/objectstorage/v3
```

```go
import objectstorage "github.com/precision-soft/melody/integrations/objectstorage/v3"
```

## Usage

```go
client, clientErr := objectstorage.NewClient(objectstorage.Config{
	Endpoint:  "s3.example.com",
	AccessKey: "...",
	SecretKey: "...",
	Secure:    true,
})
if nil != clientErr {
	return clientErr
}

if ensureErr := objectstorage.EnsureBucket(ctx, client, "documents", ""); nil != ensureErr {
	return ensureErr
}

store := objectstorage.NewStorage(client, "documents")

putErr := store.Put(runtimeInstance, "labels/awb-123.pdf", reader, size, storagecontract.PutOptions{
	ContentType: "application/pdf",
})

url, _ := store.PresignedUrl(runtimeInstance, "labels/awb-123.pdf", 15*time.Minute)
```

### Plug-and-play registration

Register the S3 backend under the core `storage.ServiceStorage` service name in one call, so handlers resolve it from the container with `storage.StorageMustFromResolver`:

```go
objectstorage.RegisterStorageService(registrar, client, "documents")
```

## Footguns & caveats

- `Put` forwards the provided size to MinIO; pass `-1` when the size is unknown and the client will stream the object.
- `Get` returns the object's reader after a `Stat`, so a missing object fails fast instead of erroring only on first read. Close the reader.
- `PresignedUrl` issues a presigned GET URL valid for the given expiry.
- The integration test (`storage_test.go`) is skipped unless `MINIO_ENDPOINT` (and `MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY`) are set; it was verified against MinIO.
