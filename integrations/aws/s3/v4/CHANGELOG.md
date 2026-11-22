# Changelog

All notable changes to `precision-soft/melody/integrations/aws/s3/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major, `integrations/awss3/v3`, up to v3.1.0; that history is in the third major's changelog.
- `storage.go`, `provider.go`, `module.go`: `StorageConfig` of `PutSpoolLimit`, what a `Put` may hold in memory, 16 MiB when left out as the constant was, at least 1, unlimited refused, and `MultipartAbortTimeout`, the bound of the detached abort, 10s when left out as the constant was, at least 1ms, unlimited refused; `EnsureBucketConfig` of `Region`; `ModuleConfig.Storage`.
- `storage.go`: `Storage.Stat(runtime, key)` answers the object's `storagecontract.ObjectInfo` from one `HEAD`; `PresignedUrl` signs an upload link under `PresignConfig{Method: http.MethodPut}`; `Get` reads `GetOptions.Range` with one `GET` from the offset, a range past the end refused with `storagecontract.ErrRangeNotSatisfiable` before it; a negative range refused before any request; an absent key answers `storagecontract.ErrObjectNotFound` in the chain at `Get` and `Stat`, confirmed by one `HEAD` of the bucket after the object's 404, so a missing bucket answers a failure. See the framework's `UPGRADE.md`.
- `module.go`, `service_resolver.go`: `ModuleConfig.BucketCheck`, a `BucketCheckConfig{Enabled, Region, BootFailure}`: enabled, the storage's provider ensures the bucket under the runtime of whoever resolves the storage before answering it, and the storage is built at boot with the declared mode, the zero value warning and the first use checking the bucket again; a bucket check without a client, and a `Region` or a `BootFailure` without `Enabled`, are refused naming the field.
- `module.go`, `plaintext_endpoint.go`: the module is a `BootWarningModule`: it declares `aws.s3.plaintextEndpoint`, exported as `PlaintextEndpointBootWarningName`, as a boot warning a command writes, and over a plaintext client off this machine adds the warning at boot, so `MELODY_CLI_BOOT_WARNINGS_IGNORED` accepts the code and both cli keys silence it; a storage built with `NewStorage` honours the two keys at its first use.

### Changed

- **Breaking** — `storage.go`: every door of `Storage` refuses a nil runtime, and one carrying no context, first, answering `the runtime is nil or carries no context` before any request is sent, where a nil runtime reached a dereference; see the framework's `UPGRADE.md`.

- **Breaking** with the framework: the fourth major's runtime is read from `v4/container/contract` and built through `v4/container/runtime`, a runtime resolving through its scope only; `runtime.New` and `Runtime.Container()` are gone. See the framework's `UPGRADE.md`.
- `module.go`: **Behavioural change**: `RegisterServices` refuses a `Bucket` or a `Storage` with no client, naming the field, where it registered nothing.
- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
- **Breaking** — `provider.go`: the endpoint is `NewClient(endpoint, config)`'s argument, an empty one refused naming `argument: endpoint`, where it was `Config.Endpoint`, which is removed and no longer printed by `Config`'s rendering.
- **Breaking** — `provider.go`: `Config.Secure` is replaced by `Config.Insecure`, so a configuration that names neither dials https where it dialed plaintext http; the refusals of an empty endpoint or credential name the field.
- **Breaking** — `provider.go`: `EnsureBucket(runtimeInstance, client, bucket, config)` takes the runtime in place of a context, its three calls bounded by the runtime's context, a nil runtime refused.
- **Breaking** — `provider.go`, `storage.go`, `service_resolver.go`: `EnsureBucket` takes an `EnsureBucketConfig` after the bucket where it took the region, `NewStorage` a `StorageConfig` after the bucket, refused naming the field, and `RegisterStorageService` a `StorageConfig` resolved at the call, so a refused value fails the registration.
- **Breaking** — `storage.go`: `Storage.Exists` is removed with the contract's door; `storage.Exists(storage.For(runtime, store), key)` answers it from `Stat`.
- **Breaking** — `storage.go`: `Storage.Get(runtime, key, storagecontract.GetOptions)` answers a `storagecontract.Object`, its `Info` read from the `HEAD` the get already sends, and `Storage.PresignedUrl(runtime, key, storagecontract.PresignConfig)` takes the expiry as a `bound.Duration`, 15 minutes when left out where `0` was refused by minio, and a value outside 1s to 7 days is refused naming `PresignConfig.Expiry`. See the framework's `UPGRADE.md`.
- **Breaking** — `module.go`, `plaintext_endpoint.go`, `provider.go`: the module is named `aws.s3`, its boot warning `aws.s3.plaintextEndpoint`, and a `Config` prints as `aws.s3.Config{…}`, after the module's path, where they said `awss3`, so a journal filter on `awss3.plaintextEndpoint` matches nothing and `MELODY_CLI_BOOT_WARNINGS_IGNORED` naming it is refused at boot as an unknown code. The Go package stays `awss3`.

### Fixed

- `storage.go`: `Put` of a body of unknown size, a negative `size`, names a part size of 16 MiB, where the part size was left to the client, which sizes it for the largest object s3 holds and allocated a buffer of about 528 MiB for every such put; an object put this way is bounded at ten thousand parts, 156.25 GiB (160,000 MiB).
- `plaintext_endpoint.go`: the plaintext endpoint warning names `Config.Insecure`, where it told the reader to set `Secure`, a field the fourth major no longer has.
- `plaintext_endpoint.go`: the plaintext endpoint warning is written once per endpoint host per process, where each storage over the same client wrote its own, so an application registering the module and building a storage of its own over one client wrote it twice.
- storage: an upload body's read error whose `Unwrap` chain reaches itself no longer holds the goroutine that checks the body's end.

### Security

- `go.mod`: `golang.org/x/crypto` v0.52.0, `golang.org/x/net` v0.55.0 and `github.com/klauspost/compress` v1.18.7, past the versions the advisories name. None of them reached code this module calls.
