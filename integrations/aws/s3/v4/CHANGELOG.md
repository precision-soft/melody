# Changelog

All notable changes to `precision-soft/melody/integrations/aws/s3/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major, `integrations/awss3/v3`, up to v3.1.0; that history is in the third major's changelog.
- `storage.go`, `provider.go`, `module.go`: `StorageConfig` of `PutSpoolLimit`, what a `Put` may hold in memory, 16 MiB when left out as the constant was, at least 1, unlimited refused, and `MultipartAbortTimeout`, the bound of the detached abort, 10s when left out as the constant was, at least 1ms, unlimited refused; `EnsureBucketConfig` of `Region`; `ModuleConfig.Storage`.

### Changed

- `module.go`: **Behavioural change**: `RegisterServices` refuses a `Bucket` or a `Storage` with no client, naming the field, where it registered nothing.
- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
- **Breaking** — `provider.go`: `Config.Secure` is replaced by `Config.Insecure`, so a configuration that names neither dials https where it dialed plaintext http; the refusals of an empty endpoint or credential name the field.
- **Breaking** — `provider.go`, `storage.go`, `service_resolver.go`: `EnsureBucket` takes an `EnsureBucketConfig` after the bucket where it took the region, `NewStorage` a `StorageConfig` after the bucket, refused naming the field, and `RegisterStorageService` a `StorageConfig` resolved at the call, so a refused value fails the registration.

### Fixed

- storage: an upload body's read error whose `Unwrap` chain reaches itself no longer holds the goroutine that checks the body's end.

