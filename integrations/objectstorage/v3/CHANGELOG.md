# Changelog

All notable changes to `precision-soft/melody/integrations/objectstorage` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial Melody v3 binding of the object storage integration — an S3-compatible implementation of the core `storage/contract.Storage` backed by `minio-go`. Developed v3-first; v1 and v2 bindings to follow.
- `provider.go` — `NewClient(Config)` (endpoint, access/secret key, secure, region) and `EnsureBucket(ctx, client, bucket, region)`.
- `storage.go` — `Storage` implementing `Put`, `Get` (with a `Stat` existence check that distinguishes a missing object — `NoSuchKey` — from transient errors such as permission/network), `Delete`, `Exists` (maps `NoSuchKey` to `false`), and `PresignedUrl`.
- `storage_test.go` — put/get/exists/presign/delete integration test, skipped unless `MINIO_ENDPOINT` is set; verified against MinIO.
