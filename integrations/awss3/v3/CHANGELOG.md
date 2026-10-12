# Changelog

All notable changes to `precision-soft/melody/integrations/awss3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- documentation: the GoDoc of the size-checked upload reader says to hand a body a timeout bounds, since the context is read between reads only and a source whose `Read` blocks is ended by its own closer, where it said neither a stalled body nor a departed client could pin an upload; and the refusal of an empty credential carries the behavioural-change marker
- `provider.go` — `Config` redacts its `AccessKey` and `SecretKey` on every `fmt` verb through new `String` and `Format` methods, rendering its safe fields with each secret shown only as set-or-not, mirroring the `encrypt` key provider, where both, exported, printed the object-storage credentials in the clear through a plain `%v` dropped into a log or an error context
- **Behavioural change**: `provider.go` — `NewClient` refuses an empty access key or secret key instead of silently downgrading to anonymous access, naming the endpoint and which credential is missing: minio treats either empty credential as a request for the anonymous signer, so a missing credential env var booted cleanly, served reads off any public bucket policy, and failed writes much later with a bare `AccessDenied`; a caller who wants anonymous access builds the minio client directly
- `provider.go`, `service_resolver.go`, `storage.go` — `EnsureBucket` no longer fails the losing replica of a bucket-creation race: on `BucketAlreadyOwnedByYou`/`BucketAlreadyExists` the bucket's existence is re-checked and only a probe confirming the bucket is this caller's reads as success, a name owned by another account still failing, and a re-check that could not complete is reported with both causes rather than as the conflict alone; `RegisterStorageService` validates the client and the bucket at registration, where `NewStorage` panics on a nil client or an empty bucket inside the closure at the first resolution, possibly on a request path, instead of at boot; every storage error names the bucket beside the key, so a process wired over more than one bucket can attribute a failure; and a put whose request context is cancelled no longer leaves an incomplete multipart upload behind, the abort running on a detached context with its own deadline, where it rode the cancelled context, never left the process and the orphan billed indefinitely
- `storage.go` — the size check's end-of-body probe surfaces a transport failure instead of folding it into a clean end, where a body that broke exactly at the declared boundary answered `io.EOF`, fabricating an exact-match verdict for a body whose length past the boundary is unknown
- storage: `Put` enforces the declared size without ever holding the body in memory, and a wrongly declared size can no longer leave a truncated object at the key: it uploaded first and probed the reader afterwards, so a `size` shorter than the body was detected after an S3 put had atomically replaced the key, the remediating `RemoveObject` then destroyed the previous object on a bucket without versioning, and a refused remediation (no `s3:DeleteObject`, a cancelled context) was discarded with the truncated object live; a seekable body is measured in place and streamed with nothing buffered, a body that cannot seek, an `http.Request.Body`, is checked as MinIO consumes it and cut off before its last declared byte the moment more body is found, leaving the upload short of its own content length, so a single-shot request is refused by the bucket and a multipart upload is aborted, and bodies declared at or below MinIO's 16 MiB part size are validated in full before any request, since MinIO uploads those as one committed request, so memory never scales with the body and the spool never exceeds the part buffer MinIO itself allocates for the same upload; the size check no longer treats a legal `(0, nil)` read as the end of the body, nor spins on one, consecutive empty reads being bounded and every read honouring the runtime context, so a stalled body or a departed client fails the put instead of pinning a core, while a correct size, a negative "unknown length" size, a zero declared size and a body shorter than its declared size behave exactly as before

### Security

- `plaintext_endpoint.go`, `storage.go`, `provider.go` — a storage over a plain http endpoint other than `localhost` or a loopback address is named once in the configured journal at the first use of a door (`bootWarning=awss3.plaintextEndpoint`, the endpoint's host, never a credential), since `Config.Secure`'s zero value sends credentials and objects in clear and presigns `http://` urls; the GoDoc of `Config.Secure` says so
- **Behavioural change**: `storage.go` — every door (`Put`, `Get`, `Delete`, `Exists`, `PresignedUrl`) refuses a key that carries a `..` segment, the backslash read as a separator, naming the segment before the bucket is asked, as `LocalStorage` does: the key was folded instead, so `tenant-b/../tenant-a/secret.txt` addressed `tenant-a/secret.txt` across a key-prefix isolation; a key relying on the fold to address `a/../b` as `b` is now refused, address it as `b`

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
