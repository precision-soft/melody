# STORAGE

The [`storage`](../../storage) package provides an object-storage abstraction. The core ships a dependency-free filesystem implementation; an S3-compatible implementation (MinIO/AWS S3) lives in the [`awss3`](../../../integrations/aws/s3/v4) integration.

## Scope

Storage is opt-in. Userland builds a `Storage` and registers it under [`ServiceStorage`](../../storage/service_resolver.go). The contract is backend-agnostic, so a handler that saves an AWB PDF or a label works the same against local disk in development and S3 in production.

## Subpackages

- [`storage/contract`](../../storage/contract)\
  Public contract for the storage abstraction.

## Responsibilities

- Define the abstraction:
    - [`Storage`](../../storage/contract/storage.go) — `Put`, `Get`, `Stat`, `Delete`, `PresignedUrl`
    - [`PutOptions`](../../storage/contract/storage.go), [`GetOptions`](../../storage/contract/storage.go) with its [`ByteRange`](../../storage/contract/storage.go), [`Object`](../../storage/contract/storage.go), [`ObjectInfo`](../../storage/contract/storage.go), [`PresignConfig`](../../storage/contract/storage.go)
    - [`ResolveByteRange`](../../storage/contract/range.go), [`PresignExpiryRule()`](../../storage/contract/presign.go), [`ResolvePresignExpiry`](../../storage/contract/presign.go), [`ResolvePresignMethod`](../../storage/contract/presign.go)
    - [`ErrKeyIsDirectory`](../../storage/contract/error.go) — the cause of a refusal of a key that names a directory, a prefix of other keys and not an object
    - [`ErrObjectNotFound`](../../storage/contract/error.go) — the cause of a `Get` or a `Stat` of a key that names no object
    - [`ErrRangeNotSatisfiable`](../../storage/contract/error.go) — the cause of a `Get` whose range starts at or past the end of the object
    - [`ErrPresignUnsupported`](../../storage/contract/error.go) — the cause of a presigned url the backend cannot sign, or cannot sign for the method asked
- Provide a filesystem implementation:
    - [`LocalStorage`](../../storage/local.go), [`NewLocalStorage`](../../storage/local.go)
- Provide the bound view:
    - [`Bound`](../../storage/bound.go), [`For`](../../storage/bound.go), [`FromRuntime`](../../storage/bound.go)
    - [`Exists`](../../storage/exists.go) — whether a key names an object, derived from `Stat`
- Provide container resolver helpers:
    - [`ServiceStorage`](../../storage/service_resolver.go)
    - [`StorageMustFromResolver`](../../storage/service_resolver.go)

## Usage

```go
store := storage.NewLocalStorage("/var/lib/app/objects", storage.LocalStorageConfig{})

labels := storage.For(runtimeInstance, store)

putErr := labels.Put("labels/awb-123.pdf", reader, size, storagecontract.PutOptions{
    ContentType: "application/pdf",
})

object, getErr := labels.Get("labels/awb-123.pdf", storagecontract.GetOptions{})
if nil == getErr {
    defer object.Body.Close()
}

objectInfo, statErr := labels.Stat("labels/awb-123.pdf")

exists, existsErr := storage.Exists(labels, "labels/awb-123.pdf")

link, presignErr := labels.PresignedUrl("labels/awb-123.pdf", storagecontract.PresignConfig{Expiry: bound.Of(10 * time.Minute)})
```

A handler that resolves the application's storage binds it in one call: `storage.FromRuntime(runtimeInstance)` resolves `ServiceStorage` through the runtime's scope and answers a `storage.Bound`.

For S3-compatible object storage, use the [`awss3`](../../../integrations/aws/s3/v4) integration, which implements the same contract over `minio-go`:

```go
client, _ := awss3.NewClient(awss3.Config{Endpoint: "s3.example.com", AccessKey: "...", SecretKey: "..."})
store := awss3.NewStorage(client, "documents", awss3.StorageConfig{})
```

## The bound view

Every door of `storagecontract.Storage` takes the runtime it runs under first; that is the contract a backend, a decorator or a fake implements. The awss3 `Storage` runs every call under the runtime's context; `LocalStorage` reads no context, since a local file operation takes none, so a cancelled runtime neither refuses its doors nor stops a copy or a read of its body. Userland calls it through `storage.Bound`, a view bound once to its request, run or delivery, whose methods take no runtime.

- `storage.For(runtimeInstance, storageInstance)` binds a storage the caller already holds, the form a service writes: it keeps the unbound storage in a field and binds it at the top of each method.
- A view is a value of one runtime: it is never kept in a service, a field or a goroutine that outlives the runtime, and no provider registers one, so the wiring generator refuses a constructor that asks for a `storage.Bound`.
- The `Object.Body` a `Get` answers is read after the door returned, under the bound runtime's context on a backend that reads one, the awss3 `Storage`; a call that needs another deadline, or one that must outlive the caller's cancellation, binds again over `runtime.WithContext(runtimeInstance, ctx)`.
- A view over no storage, a nil or a typed nil, refuses every door with `storage instance is nil`. A nil runtime is handed to the backend as it is, and both shipped backends refuse it first at every door, with `the runtime is nil or carries no context`, before the local storage touches its base directory and before the S3 backend sends a request. Binding and calling allocate nothing of their own; `storage.FromRuntime` costs the by-name resolution, so a function binds once and calls many times.
- `Runtime()` and `Storage()` answer the two halves the view is bound over.

## Footguns & caveats

- Storage is opt-in and userland-wired; the framework registers no default storage.
- A key is folded before it addresses an object, alike on [`LocalStorage`](../../storage/local.go) and the awss3 `Storage`: a backslash reads as a separator, and `.` and empty segments and a leading slash fold away. A key that carries a `..` segment is refused by name at every door instead of being folded, so `tenant-b/../tenant-a/secret.txt` never addresses `tenant-a/secret.txt`. An application that isolates its callers by key prefix can still not hand a client-chosen name to a door unchecked: it must refuse a name carrying a separator itself.
- [`NewLocalStorage`](../../storage/local.go) creates and opens its base directory at construction, so a path it cannot create or open, a file in its place included, panics there naming `baseDirectory`; resolving the service creates the directory, in a cli process too. No handle is kept: every operation reopens the directory, and `Put` creates it again if it was removed.
- [`LocalStorage`](../../storage/local.go) sanitizes keys against path traversal (a key resolving outside the base directory is rejected, including via a symlink: every operation runs through an `os.OpenRoot`-pinned `*os.Root`, which follows a link only while it stays inside the base and refuses one that leaves it, and `Put` refuses a key whose leaf is a symlink rather than replace it) and does not support `PresignedUrl`: it answers an error whose chain carries [`storagecontract.ErrPresignUnsupported`](../../storage/contract/error.go), so a caller that falls back to streaming the object through the application branches on `exception.ChainIs`.
- `Put` takes the content size; pass `-1` when the size is unknown (S3 backends stream it, `LocalStorage` skips its length check). When a non-negative size is given, `LocalStorage` enforces it — a reader that does not yield exactly that many bytes returns an error. A failed `Put` leaves no object behind: `LocalStorage` removes the partially-written file on any write failure, including a size mismatch and a source reader that errors mid-stream (so a streaming `-1` upload whose reader fails does not leave a truncated object), matching the S3 backend's atomic-on-failure behaviour. What the in-process error paths cannot clean is a crash mid-write: the temp object it leaves behind is swept by a later `Put` of the same key, once its modification time is at least `LocalStorageConfig.TemporaryStaleAge` old, an hour when left out. Temp objects live in a reserved hidden namespace beside the key, `.melody-storage-<sha256 of the key's leaf>.<16 hex>.tmp`, which fits one path component whatever the leaf's length; only regular files of exactly that
  shape for that key are ever swept, so a concurrent writer's live temp, another key's temp and an ordinary key that reads like a temp name, such as `report.tmp-0123456789abcdef`, are never touched. A key whose leaf is spelled exactly like a temp object is refused at every door, since a later `Put` of the key its digest names would sweep it. A temp object a crash left under the naming of `v3.13.0`, `<leaf>.tmp-<16 hex>`, is not of the reserved shape and is never swept; remove it by hand.
- On [`LocalStorage`](../../storage/local.go), a declared size bounds the copy at one byte past it, so a reader that runs longer is refused without being read to its end; a nil reader is refused before anything is created. A failed `Put` removes its temp object but keeps the directories it created for the key. A `Put` over an existing object replaces it with a new file of mode `0640` less the process umask, whatever the old one's mode. A writer that stalls for more than the stale age between two writes may have its temp object swept by a concurrent `Put` of the same key; its own `Put` then fails naming the sweep.
- Locally a key names an object only when it resolves to a regular file. A key that is a directory is a prefix of other keys: `Get`, `Stat`, `Put` and `Delete` refuse it with [`storagecontract.ErrKeyIsDirectory`](../../storage/contract/error.go) in the chain, `Put` before it reads the payload and `Delete` removing nothing, and [`storage.Exists`](../../storage/exists.go) answers `false`, since a directory is not an object. A symlink to a directory is refused by `Get` and `Stat`, which follow it, and removed as a link by `Delete`, which does not, its target kept. A key that runs through a regular file (`object.bin/child`) names no object, since locally an object and a prefix cannot share a name, where S3 allows both: `Get` and `Stat` answer `ErrObjectNotFound`, `Exists` answers `false` and `Delete` answers `nil`. A named pipe or a device under a key is refused by `Get` at once and by `Stat` without opening it, and `Exists` answers `Stat`'s refusal beside `false`.
- [`storage.Exists(boundStorage, key)`](../../storage/exists.go) is no door of the contract: it answers `true` when `Stat` answers, `false` when `Stat` answers `ErrObjectNotFound` or `ErrKeyIsDirectory`, and `Stat`'s error beside `false` for anything else, a refused key included, so it cannot drift from what `Stat` and `Get` judge.
- A key that names no object answers an error whose chain carries [`storagecontract.ErrObjectNotFound`](../../storage/contract/error.go) at `Get` and `Stat` on both backends; a refusal of the key and a failure to ask the backend never carry it. Locally a base directory that is gone reads as absence too; on S3 a missing bucket is a failure, never an absent key, the absence being confirmed by one `HEAD` of the bucket after the object's 404. `Delete` of such a key answers `nil`: the object is gone, which is what was asked.
- `Get` answers a [`storagecontract.Object`](../../storage/contract/storage.go): the caller closes `Object.Body`, exactly once, also after a read failed; beside an error the object is the zero value and nothing is open. `Object.Info` and `Stat` answer the same [`ObjectInfo`](../../storage/contract/storage.go): `Size`, `ContentType` (what the `Put` declared; when it declared none, `LocalStorage` answers it empty and the awss3 `Storage` the store's default, `application/octet-stream`), `ModifiedAt`, `EntityTag` without quotes and `Key`, the key asked as given. The local entity tag is derived from the size and the modification time as the static file server derives its own, so it is weak-equivalent: two stores of equal size in the same instant share it.
- [`LocalStorage`](../../storage/local.go) keeps the content type a `Put` declares in a metadata object of the reserved namespace beside the object, `.melody-storage-<sha256 of the key's leaf>.metadata`, a json object of the content type and the size and modification time of the write it describes; a key spelled like it, or like a temp object, in any of its segments is refused at every door. A content type whose metadata object could pass 4 KiB, more than 4009 bytes of plain ascii, is refused by `Put` naming `PutOptions.ContentType` before anything is created. It is written after the object's rename, so a `Put` whose rename failed leaves the previous object and its content type as they were, and a `Put` whose metadata could not be written answers an error with the object stored and read as no content type. It is honoured only while the object keeps that size and modification time, so a read that falls between the object's rename and the metadata's, a crash between the two, two writers interleaving, an object replaced or touched by hand, a restore that did not keep the modification time, a metadata object that is not json or larger than 4 KiB all read as no content type, never as an error or as another object's type. A `Put` that declares none removes it, `Delete` removes it after the object, and an object stored before this major answers an empty content type until it is stored again. A tool that lists the base directory sees the metadata objects; the entity tag is derived, never stored.
- `GetOptions.Range` reads a part of the object, `ByteRange{Offset, Length}`, judged by [`storagecontract.ResolveByteRange`](../../storage/contract/range.go) on both backends: the zero value is the whole object, a zero `Length` reads to the end and one past the end is clipped to it, a negative field is refused naming `GetOptions.Range` by [`storagecontract.RefuseNegativeByteRange`](../../storage/contract/range.go) before the backend is asked, a missing key included, and an `Offset` at or past the end answers `ErrRangeNotSatisfiable`, the offset 0 of an empty object excepted. `Object.Info.Size` stays the whole object's size. A suffix range, the last N bytes, and several ranges are not expressible. An implementation of the application's own honours the range: one that ignores it answers the whole object to a caller that asked a part.
- `PresignedUrl` takes a [`storagecontract.PresignConfig`](../../storage/contract/storage.go). `Expiry` is read against `PresignExpiryRule()`: 15 minutes when left out, at least 1s, at most 7 days, unlimited refused, each refusal naming `PresignConfig.Expiry`. `Method` is `http.MethodGet`, a download, when left out, or `http.MethodPut`, an upload of the key; any other method is refused with `ErrPresignUnsupported`. A presigned upload binds neither a content type nor a size.

## Userland API

### Contracts (`storage/contract`)

- [`Storage`](../../storage/contract/storage.go)
- [`PutOptions`](../../storage/contract/storage.go), [`GetOptions`](../../storage/contract/storage.go), [`ByteRange`](../../storage/contract/storage.go), [`Object`](../../storage/contract/storage.go), [`ObjectInfo`](../../storage/contract/storage.go), [`PresignConfig`](../../storage/contract/storage.go)
- [`RefuseNegativeByteRange(ByteRange) error`](../../storage/contract/range.go), [`ResolveByteRange(ByteRange, int64) (int64, int64, error)`](../../storage/contract/range.go)
- [`PresignExpiryRule()`](../../storage/contract/presign.go), [`ResolvePresignExpiry(bound.Duration) (time.Duration, error)`](../../storage/contract/presign.go), [`ResolvePresignMethod(string) (string, error)`](../../storage/contract/presign.go)
- [`ErrKeyIsDirectory`](../../storage/contract/error.go), [`ErrObjectNotFound`](../../storage/contract/error.go), [`ErrRangeNotSatisfiable`](../../storage/contract/error.go), [`ErrPresignUnsupported`](../../storage/contract/error.go)

### Types and constructors (`storage`)

- [`LocalStorage`](../../storage/local.go) — [`NewLocalStorage(baseDirectory string, config LocalStorageConfig) *LocalStorage`](../../storage/local.go) — [`LocalStorageConfig`](../../storage/local.go) holds `TemporaryStaleAge`, 1h when left out, at least 1s, unlimited never sweeping, and `SweepBatchSize`, the names of a key's directory the sweep reads at a time, 256 when left out, at least 1, unlimited refused; a field outside its rule is refused with a panic naming it

### Bound view (`storage`)

- [`Bound`](../../storage/bound.go) — [`For(containercontract.Runtime, storagecontract.Storage) Bound`](../../storage/bound.go), [`FromRuntime(containercontract.Runtime) Bound`](../../storage/bound.go)
- [`Exists(Bound, string) (bool, error)`](../../storage/exists.go)

### Container helpers (`storage`)

- [`const ServiceStorage`](../../storage/service_resolver.go)
- [`StorageMustFromResolver(context.Context, containercontract.Resolver) storagecontract.Storage`](../../storage/service_resolver.go)
