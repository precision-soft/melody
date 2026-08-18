# Changelog

All notable changes to `precision-soft/melody/integrations/awss3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- a put whose request context is cancelled no longer leaves an incomplete multipart upload behind. Over the spool limit the object goes up in parts, and the abort the client library issues on failure rode the same context the cancellation had already closed, so the abort never left the process and the orphan billed indefinitely. The abort now runs on a detached context with its own deadline, so it leaves whatever happened to the request and still cannot hang
- storage: `Put` enforces the declared size without ever holding the body in memory, and a wrongly declared size can no longer leave a truncated object at the key. It previously uploaded first and probed the caller's reader afterwards, so a `size` shorter than the body was detected only after an S3 put had atomically replaced the key with the truncated body — and the remediating `RemoveObject` then deleted what was left, destroying any previously stored object beyond recovery on a bucket without versioning; if that remediation was refused (no `s3:DeleteObject`, or a cancelled context) its error was discarded, leaving the truncated object live while the caller was told the put had failed. A seekable body is now measured in place and streamed with nothing buffered. A body that cannot seek — an `http.Request.Body`, the natural argument of this call — is checked as MinIO consumes it and is cut off before its last declared byte the moment more body is found behind it, which leaves the
  upload short of its own content length: a single-shot request is refused by the bucket and a multipart upload is aborted, so nothing reaches the key. Bodies declared at or below MinIO's 16 MiB part size are validated in full before any request is issued, since MinIO uploads those as one committed request; memory therefore never scales with the body, and the spool never exceeds the part buffer MinIO itself allocates for the same upload
- storage: the size check no longer treats a legal `(0, nil)` read as the end of the body — an over-read could go undetected and a silently truncated object was stored with `Put` reporting success — and it no longer spins on one either: consecutive empty reads are bounded and every read honours the runtime context, so a stalled body or a client that walked away fails the put instead of pinning a core and an upload. A correct size, a negative "unknown length" size, a zero declared size, and a body shorter than its declared size all behave exactly as before

## [v3.0.3] - 2026-07-06 - Standalone Module Resolution Fix

### Fixed

- `go.mod` — the module pinned `melody/v3 v3.0.0` while importing the `storage`/`storage/contract` packages, which only exist from `v3.7.0`: outside the repository workspace the module did not resolve. The pin is raised to `v3.7.0` — the lowest framework version that provides every imported package — and the module-local `go.sum` is now complete for standalone builds.

## [v3.0.2] - 2026-06-25 - Put Over-Read Guard Reader-Type Fix

### Fixed

- `storage.go` — the v3.0.1 `Put` over-read guard probed the caller's `reader` after `minio.PutObject` to detect a body longer than its declared `size`, but misfired on every valid `Put` of an `io.ReaderAt`+`io.Seeker` reader (`*bytes.Reader`, `*strings.Reader`, a non-stdio `*os.File` — the dominant callers). minio's single-shot `putObject` wraps such a reader in an `io.SectionReader` and uploads the body via `ReadAt`, which does **not** advance the caller's sequential `Read` cursor; the post-upload probe therefore read byte 0 of a correctly-sized body, returned a spurious "size does not match the declared size" error, **and `RemoveObject`-deleted the object it had just stored** — silent data loss for valid input. `Put` now hands minio the body through `boundedPutReader` — an `io.LimitReader` that is neither an `io.ReaderAt` nor an `io.Seeker` — forcing minio's sequential path to consume exactly `size` bytes straight from the caller's reader (and bounding what it stores at the declared
  size), so the trailing-byte probe is accurate for every reader type. A negative size still streams the whole reader.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/awss3/v3.0.3...HEAD

[v3.0.3]: https://github.com/precision-soft/melody/compare/integrations/awss3/v3.0.2...integrations/awss3/v3.0.3

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/awss3/v3.0.1...integrations/awss3/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/awss3/v3.0.0...integrations/awss3/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/awss3/v3.0.0
