# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.1.1] - 2026-07-25 - Contained Manager Registry Teardown

### Fixed

- the manager registry no longer wedges permanently when opening a database panics while the registry is being closed. The section that publishes the opened manager released its lock without a defer, so a panic from the database's own `Close` unwound with the lock held and the recovery path then blocked on that same lock — after which every later call to the registry blocked forever, silently. A provider returning a nil database was enough to trigger it

## [v2.1.0] - 2026-07-23 - Spelled-Out Connection Parameters

### Changed

- `connection_parameters.go` — `ConnectionParameters` spells the name out; `ConnectionParams` remains as a deprecated alias, so nothing breaks at compile time.

## [v2.0.1] - 2026-07-11 - Manager Registry Open Concurrency and Panic Safety

### Fixed

- `manager_registry.go` — a panic inside `Provider.Open` no longer wedges every later caller. `Manager` opens outside the registry lock and coalesces concurrent callers of the same name onto one open, so when a mis-parameterised provider panicked (the pgsql/mysql providers resolve parameters with `MustGet`) the deletion of the in-flight entry and the close of its done channel were skipped, and a recovered panic left every subsequent `Manager(name)` blocked forever on the never-closed channel. The open is now wrapped so the slot is always cleared and waiters are released with an error, while the panic still propagates to the direct caller.
- `manager_registry.go` — a database opened while `Close` is running is no longer leaked. `Close` drains the manager map without an open still in flight, so an open completing afterwards memoised its `*bun.DB` into a registry nobody would drain again; `Manager` now closes the freshly opened database and returns `ErrManagerRegistryClosed` when it finds the registry closed on completion.
- `manager_registry.go` — `ManagerRegistry` no longer holds its internal lock across `Provider.Open`. A slow or unreachable database otherwise serialized `Manager`/`Database`/`Close` for every other database and stalled shutdown; concurrent openers of the same manager now coalesce onto one open, and a failed open is not cached.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Provider.Open Signature Change

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/v2` — Go v2 migration
- `provider.go` — `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads from container; caller supplies pre-built params and a logger

### Changed

- Code migrated into `integrations/bunorm/v2/` with matching module path
- `go.mod` — dependency on `github.com/precision-soft/melody` bumped from v1.3.2 to v1.6.3

### Added

- `connection_params.go` — `bunorm.ConnectionParams` struct (`Host`, `Port`, `Database`, `User`, `Password`) with `SafeContext()` method that elides the password for logging
- `provider_definition.go` — `ProviderDefinition.Params` field holds connection parameters separately from the definition name

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.1.1...HEAD

[v2.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.1.0...integrations/bunorm/v2.1.1

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.0.1...integrations/bunorm/v2.1.0

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.0.0...integrations/bunorm/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v2.0.0
