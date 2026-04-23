# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.1] - 2026-03-08 - Tidy v3 go.sum Dependencies

### Changed

- `v3/go.sum` — resolved transitive dependency checksums; no API changes

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/v3` — Go v3 migration; consumers must update imports from `/integrations/bunorm/v2` to `/integrations/bunorm/v3`

### Changed

- Code duplicated into `integrations/bunorm/v3/`; `go.mod` module path updated
- Dependencies pinned to `github.com/precision-soft/melody/v3` and other v3 module paths
- README relative path links updated to reflect v3 directory structure

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

## [v1.0.0] - 2026-02-05 - Initial Release — Bun ORM Integration

### Added

- `provider.go` — `bunorm.Provider` — dialect-agnostic database provider interface
- `provider_definition.go` — `bunorm.ProviderDefinition` — registers multiple database providers with default-provider support
- `manager_registry.go` — `bunorm.ManagerRegistry` — caches and manages `*bunorm.Manager` instances (1:1 per provider definition); exposes `Manager(name)` / `MustManager(name)` / `DefaultManager()` / `MustDefaultManager()` / `DefaultDatabase()` / `MustDefaultDatabase()` accessors
- `manager.go` — `bunorm.Manager` — owns a single `*bun.DB`; exposes `Database()` and `Close()` methods
- `errors.go` — error sentinels: `ErrResolverIsRequired`, `ErrNoProviderDefinitions`, `ErrProviderDefinitionNameIsRequired`, `ErrProviderIsRequired`, `ErrProviderDefinitionNameMustBeUnique`, `ErrMultipleDefaultProviderDefinitions`
- `README.md` — service registration patterns

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.0...integrations/bunorm/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.0.0...integrations/bunorm/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v1.0.0...integrations/bunorm/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fv1.0.0
