# STORAGE

The [`storage`](../../storage) package provides an object-storage abstraction. The core ships a dependency-free filesystem implementation; an S3-compatible implementation (MinIO/AWS S3) lives in the [`awss3`](../../../integrations/awss3) integration.

## Scope

Storage is opt-in. Userland builds a `Storage` and registers it under [`ServiceStorage`](../../storage/service_resolver.go). The contract is backend-agnostic, so a handler that saves an AWB PDF or a label works the same against local disk in development and S3 in production.

## Subpackages

- [`storage/contract`](../../storage/contract)  
  Public contract for the storage abstraction.

## Responsibilities

- Define the abstraction:
    - [`Storage`](../../storage/contract/storage.go) — `Put`, `Get`, `Delete`, `Exists`, `PresignedUrl`
    - [`PutOptions`](../../storage/contract/storage.go)
- Provide a filesystem implementation:
    - [`LocalStorage`](../../storage/local.go), [`NewLocalStorage`](../../storage/local.go)
- Provide container resolver helpers:
    - [`ServiceStorage`](../../storage/service_resolver.go)
    - [`StorageMustFromContainer`](../../storage/service_resolver.go), [`StorageMustFromResolver`](../../storage/service_resolver.go)

## Usage

```go
store := storage.NewLocalStorage("/var/lib/app/objects")

putErr := store.Put(runtimeInstance, "labels/awb-123.pdf", reader, size, storagecontract.PutOptions{
	ContentType: "application/pdf",
})

reader, getErr := store.Get(runtimeInstance, "labels/awb-123.pdf")
```

For S3-compatible object storage, use the [`awss3`](../../../integrations/awss3) integration, which implements the same contract over `minio-go`:

```go
client, _ := awss3.NewClient(awss3.Config{Endpoint: "s3.example.com", AccessKey: "...", SecretKey: "...", Secure: true})
store := awss3.NewStorage(client, "documents")
```

## Footguns & caveats

- Storage is opt-in and userland-wired; the framework registers no default storage.
- [`LocalStorage`](../../storage/local.go) sanitizes keys against path traversal (a key resolving outside the base directory is rejected, including via a symlink — a leaf symlink is refused and opens use `O_NOFOLLOW`) and does not support `PresignedUrl` (it returns an error).
- `Put` takes the content size; pass `-1` when the size is unknown (S3 backends stream it, `LocalStorage` skips its length check). When a non-negative size is given, `LocalStorage` enforces it — a reader that does not yield exactly that many bytes returns an error. A failed `Put` leaves no object behind: `LocalStorage` removes the partially-written file on any write failure, including a size mismatch and a source reader that errors mid-stream (so a streaming `-1` upload whose reader fails does not leave a truncated object), matching the S3 backend's atomic-on-failure behaviour.
- `Get` returns an `io.ReadCloser` the caller must close.

## Userland API

### Contracts (`storage/contract`)

- [`Storage`](../../storage/contract/storage.go)
- [`PutOptions`](../../storage/contract/storage.go)

### Types and constructors (`storage`)

- [`LocalStorage`](../../storage/local.go) — [`NewLocalStorage(baseDirectory string) *LocalStorage`](../../storage/local.go)

### Container helpers (`storage`)

- [`const ServiceStorage`](../../storage/service_resolver.go)
- [`StorageMustFromContainer(containercontract.Container) storagecontract.Storage`](../../storage/service_resolver.go)
- [`StorageMustFromResolver(containercontract.Resolver) storagecontract.Storage`](../../storage/service_resolver.go)
