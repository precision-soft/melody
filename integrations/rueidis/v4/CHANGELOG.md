# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.6.0; that history is in the third major's changelog.

### Changed

- **Breaking** — `service_resolver.go` and `cache/service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
- **Breaking** — `cache/backend.go`, `cache/backend_service.go`: `Backend.Set`, `SetCtx`, `SetMultiple` and `SetMultipleCtx` and the `BackendService` twins take the ttl as a `bound.Duration`, the fourth major's cache contract form, read through `cache/contract.ResolveTtl`, so an absent ttl and one under a second are refused with the in-memory backend's answer and unlimited stores with no expiry, where the third major read 0 as no expiry, refused a negative ttl as `cache ttl is negative` and floored a sub-millisecond one to a millisecond.
