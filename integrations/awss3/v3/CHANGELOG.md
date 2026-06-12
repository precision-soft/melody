# Changelog

All notable changes to `precision-soft/melody/integrations/awss3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.0] - 2026-06-12 - Initial Release — S3-Compatible Object Storage

### Added

- Initial Melody v3 binding of the object storage integration — an S3-compatible implementation of the core `storage/contract.Storage` backed by `minio-go`. Developed v3-first; v1 and v2 bindings to follow.
- `service_resolver.go` — `RegisterStorageService(registrar, client, bucket)` registers the S3 backend under the core `storage.ServiceStorage`, so userland wires it into the container in one call.
- `provider.go` — `NewClient(Config)` (endpoint, access/secret key, secure, region) and `EnsureBucket(ctx, client, bucket, region)`.
- `storage.go` — `Storage` implementing `Put`, `Get` (with a `Stat` existence check that distinguishes a missing object — `NoSuchKey` — from transient errors such as permission/network), `Delete`, `Exists` (maps `NoSuchKey` to `false`), and `PresignedUrl`.
- `storage_test.go` — put/get/exists/presign/delete integration test, skipped unless `MINIO_ENDPOINT` is set; verified against MinIO.

### Fixed

- `storage.go` — object keys are now normalized the same way the core `LocalStorage` backend normalizes them (backslash to forward slash, clean `.`/`..` segments, strip the leading slash) before every `Put`/`Get`/`Delete`/`Exists`/`PresignedUrl` call. Keys were passed to S3 verbatim while `LocalStorage` cleaned them, so the same key string addressed different objects depending on the backend, and `PresignedUrl("a/../f.txt")` signed a path the browser collapses before sending (yielding `SignatureDoesNotMatch`). An empty or `.`/`..`-only key is now rejected, matching the `LocalStorage` contract.

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/awss3/v3.0.0...HEAD

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/awss3/v3.0.0
