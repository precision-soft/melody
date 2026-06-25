# Changelog

All notable changes to `precision-soft/melody/integrations/awss3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.2] - 2026-06-25 - Put Over-Read Guard Reader-Type Fix

### Fixed

- `storage.go` — the v3.0.1 `Put` over-read guard probed the caller's `reader` after `minio.PutObject` to detect a body longer than its declared `size`, but misfired on every valid `Put` of an `io.ReaderAt`+`io.Seeker` reader (`*bytes.Reader`, `*strings.Reader`, a non-stdio `*os.File` — the dominant callers). minio's single-shot `putObject` wraps such a reader in an `io.SectionReader` and uploads the body via `ReadAt`, which does **not** advance the caller's sequential `Read` cursor; the post-upload probe therefore read byte 0 of a correctly-sized body, returned a spurious "size does not match the declared size" error, **and `RemoveObject`-deleted the object it had just stored** — silent data loss for valid input. `Put` now hands minio the body through `boundedPutReader` — an `io.LimitReader` that is neither an `io.ReaderAt` nor an `io.Seeker` — forcing minio's sequential path to consume exactly `size` bytes straight from the caller's reader (and bounding what it stores at the declared size), so the trailing-byte probe is accurate for every reader type. A negative size still streams the whole reader.

## [v3.0.1] - 2026-06-25 - Put Size-Mismatch Rejection

### Fixed

- `storage.go` — `Put` forwarded `(reader, size)` straight to `minio.PutObject`, which reads exactly `size` bytes when `size >= 0` and silently ignores any trailing bytes, storing a **truncated** object and reporting success; the core `LocalStorage` backend (`written != size`) rejects a reader longer than its declared size. `Put` now detects the over-read after the upload, removes the truncated object, and returns a size-mismatch error, so the two backends sharing the `storage/contract.Storage` contract behave identically for a body longer than its declared size (a negative size still streams the whole reader with no check on both backends).

## [v3.0.0] - 2026-06-16 - Initial Release — S3-Compatible Object Storage

### Added

- Initial Melody v3 binding of the object storage integration — an S3-compatible implementation of the core `storage/contract.Storage` backed by `minio-go`. Developed v3-first; v1 and v2 bindings to follow.
- `service_resolver.go` — `RegisterStorageService(registrar, client, bucket)` registers the S3 backend under the core `storage.ServiceStorage`, so userland wires it into the container in one call.
- `module.go` — `NewModule(ModuleConfig{Client, Bucket})` self-registering application module: a single `app.RegisterModule(awss3.NewModule(...))` registers the object-storage service (skipped when the client is nil), replacing a hand-written `RegisterStorageService` call.
- `provider.go` — `NewClient(Config)` (endpoint, access/secret key, secure, region) and `EnsureBucket(ctx, client, bucket, region)`.
- `storage.go` — `Storage` implementing `Put`, `Get` (with a `Stat` existence check that distinguishes a missing object — `NoSuchKey` — from transient errors such as permission/network), `Delete`, `Exists` (maps `NoSuchKey` to `false`), and `PresignedUrl`.
- `storage_test.go` — put/get/exists/presign/delete integration test, skipped unless `MINIO_ENDPOINT` is set; verified against MinIO.

### Fixed

- `storage.go` — object keys are now normalized the same way the core `LocalStorage` backend normalizes them (backslash to forward slash, clean `.`/`..` segments, strip the leading slash) before every `Put`/`Get`/`Delete`/`Exists`/`PresignedUrl` call. Keys were passed to S3 verbatim while `LocalStorage` cleaned them, so the same key string addressed different objects depending on the backend, and `PresignedUrl("a/../f.txt")` signed a path the browser collapses before sending (yielding `SignatureDoesNotMatch`). An empty or `.`/`..`-only key is now rejected, matching the `LocalStorage` contract.

[v3.0.2]: https://github.com/precision-soft/melody/releases/tag/integrations/awss3/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/releases/tag/integrations/awss3/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/awss3/v3.0.0
