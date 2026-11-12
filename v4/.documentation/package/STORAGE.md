# STORAGE

The [`storage`](../../storage) package provides an object-storage abstraction. The core ships a dependency-free filesystem implementation; an S3-compatible implementation (MinIO/AWS S3) lives in the [`awss3`](../../../integrations/aws/s3/v4) integration.

## Scope

Storage is opt-in. Userland builds a `Storage` and registers it under [`ServiceStorage`](../../storage/service_resolver.go). The contract is backend-agnostic, so a handler that saves an AWB PDF or a label works the same against local disk in development and S3 in production.

## Subpackages

- [`storage/contract`](../../storage/contract)\
  Public contract for the storage abstraction.

## Responsibilities

- Define the abstraction:
    - [`Storage`](../../storage/contract/storage.go) — `Put`, `Get`, `Delete`, `Exists`, `PresignedUrl`
    - [`PutOptions`](../../storage/contract/storage.go)
    - [`ErrKeyIsDirectory`](../../storage/contract/error.go) — the cause of a refusal of a key that names a directory, a prefix of other keys and not an object
- Provide a filesystem implementation:
    - [`LocalStorage`](../../storage/local.go), [`NewLocalStorage`](../../storage/local.go)
- Provide container resolver helpers:
    - [`ServiceStorage`](../../storage/service_resolver.go)
    - [`StorageMustFromContainer`](../../storage/service_resolver.go), [`StorageMustFromResolver`](../../storage/service_resolver.go)

## Usage

```go
store := storage.NewLocalStorage("/var/lib/app/objects", storage.LocalStorageConfig{})

putErr := store.Put(runtimeInstance, "labels/awb-123.pdf", reader, size, storagecontract.PutOptions{
    ContentType: "application/pdf",
})

reader, getErr := store.Get(runtimeInstance, "labels/awb-123.pdf")
```

For S3-compatible object storage, use the [`awss3`](../../../integrations/aws/s3/v4) integration, which implements the same contract over `minio-go`:

```go
client, _ := awss3.NewClient(awss3.Config{Endpoint: "s3.example.com", AccessKey: "...", SecretKey: "..."})
store := awss3.NewStorage(client, "documents", awss3.StorageConfig{})
```

## Footguns & caveats

- Storage is opt-in and userland-wired; the framework registers no default storage.
- A key is folded before it addresses an object, alike on [`LocalStorage`](../../storage/local.go) and the awss3 `Storage`: a backslash reads as a separator, and `.` and empty segments and a leading slash fold away. A key that carries a `..` segment is refused by name at every door instead of being folded, so `tenant-b/../tenant-a/secret.txt` never addresses `tenant-a/secret.txt`. An application that isolates its callers by key prefix can still not hand a client-chosen name to a door unchecked: it must refuse a name carrying a separator itself.
- [`NewLocalStorage`](../../storage/local.go) creates and opens its base directory at construction, so a path it cannot create or open, a file in its place included, panics there naming `baseDirectory`; resolving the service creates the directory, in a cli process too. No handle is kept: every operation reopens the directory, and `Put` creates it again if it was removed.
- [`LocalStorage`](../../storage/local.go) sanitizes keys against path traversal (a key resolving outside the base directory is rejected, including via a symlink: every operation runs through an `os.OpenRoot`-pinned `*os.Root`, which follows a link only while it stays inside the base and refuses one that leaves it, and `Put` refuses a key whose leaf is a symlink rather than replace it) and does not support `PresignedUrl` (it returns an error).
- `Put` takes the content size; pass `-1` when the size is unknown (S3 backends stream it, `LocalStorage` skips its length check). When a non-negative size is given, `LocalStorage` enforces it — a reader that does not yield exactly that many bytes returns an error. A failed `Put` leaves no object behind: `LocalStorage` removes the partially-written file on any write failure, including a size mismatch and a source reader that errors mid-stream (so a streaming `-1` upload whose reader fails does not leave a truncated object), matching the S3 backend's atomic-on-failure behaviour. What the in-process error paths cannot clean is a crash mid-write: the temp object it leaves behind is swept by a later `Put` of the same key, once its modification time is at least `LocalStorageConfig.TempStaleAge` old, an hour when left out. Temp objects live in a reserved hidden namespace beside the key, `.melody-storage-<sha256 of the key's leaf>.<16 hex>.tmp`, which fits one path component whatever the leaf's length; only regular files of exactly that
  shape for that key are ever swept, so a concurrent writer's live temp, another key's temp and an ordinary key that reads like a temp name, such as `report.tmp-0123456789abcdef`, are never touched. A key whose leaf is spelled exactly like a temp object is refused at every door, since a later `Put` of the key its digest names would sweep it. A temp object a crash left under the naming of `v3.13.0`, `<leaf>.tmp-<16 hex>`, is not of the reserved shape and is never swept; remove it by hand.
- On [`LocalStorage`](../../storage/local.go), a declared size bounds the copy at one byte past it, so a reader that runs longer is refused without being read to its end; a nil reader is refused before anything is created. A failed `Put` removes its temp object but keeps the directories it created for the key. A `Put` over an existing object replaces it with a new file of mode `0640` less the process umask, whatever the old one's mode. A writer that stalls for more than the stale age between two writes may have its temp object swept by a concurrent `Put` of the same key; its own `Put` then fails naming the sweep.
- Locally a key names an object only when it resolves to a regular file. A key that is a directory is a prefix of other keys: `Get`, `Put` and `Delete` refuse it with [`storagecontract.ErrKeyIsDirectory`](../../storage/contract/error.go) in the chain, `Put` before it reads the payload and `Delete` removing nothing, and `Exists` answers `false`, since a directory is not an object. A symlink to a directory is refused by `Get`, which follows it, and removed as a link by `Delete`, which does not, its target kept. A key that runs through a regular file (`object.bin/child`) names no object, since locally an object and a prefix cannot share a name, where S3 allows both: `Exists` answers `false` and `Delete` answers `nil`. A named pipe or a device under a key is refused by `Get` at once and is not reported by `Exists`.
- `Get` returns an `io.ReadCloser` the caller must close.

## Userland API

### Contracts (`storage/contract`)

- [`Storage`](../../storage/contract/storage.go)
- [`PutOptions`](../../storage/contract/storage.go)
- [`ErrKeyIsDirectory`](../../storage/contract/error.go)

### Types and constructors (`storage`)

- [`LocalStorage`](../../storage/local.go) — [`NewLocalStorage(baseDirectory string, config LocalStorageConfig) *LocalStorage`](../../storage/local.go) — [`LocalStorageConfig`](../../storage/local.go) holds `TempStaleAge`, 1h when left out, at least 1s, unlimited never sweeping, and `SweepBatchSize`, the names of a key's directory the sweep reads at a time, 256 when left out, at least 1, unlimited refused; a field outside its rule is refused with a panic naming it

### Container helpers (`storage`)

- [`const ServiceStorage`](../../storage/service_resolver.go)
- [`StorageMustFromContainer(context.Context, containercontract.Container) storagecontract.Storage`](../../storage/service_resolver.go)
- [`StorageMustFromResolver(context.Context, containercontract.Resolver) storagecontract.Storage`](../../storage/service_resolver.go)
