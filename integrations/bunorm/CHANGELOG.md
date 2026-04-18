# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.1] - 2026-03-08

### Changed

- Patch release — `go.sum` updated with resolved transitive dependencies; no API changes

## [v3.0.0] - 2026-03-08

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/v3` — Go semantic-import-versioning v3 migration
- Consumers must update imports from `/integrations/bunorm/v2` to `/integrations/bunorm/v3`

### Changed

- Code duplicated into `integrations/bunorm/v3/`; `go.mod` module path updated
- Dependencies pinned to `github.com/precision-soft/melody/v3` and other v3 module paths
- README relative path links updated to reflect v3 directory structure

## [v2.0.0] - 2026-02-17

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/v2` — Go v2 migration
- `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads from container; caller supplies pre-built params and a logger

### Added

- `bunorm.ConnectionParams` struct (`Host`, `Port`, `Database`, `User`, `Password`) with `SafeContext()` method that elides the password for logging
- `ProviderDefinition.Params` field to hold connection parameters separately from the definition name

### Changed

- Code migrated into `integrations/bunorm/v2/` with `module github.com/precision-soft/melody/integrations/bunorm/v2`
- Dependency on `github.com/precision-soft/melody` bumped from v1.3.2 to v1.6.3

## [v1.0.0] - 2026-02-05

### Added

- Initial release — Bun ORM integration for Melody
- `bunorm.Provider` — dialect-agnostic database provider interface
- `bunorm.ProviderDefinition` — registers multiple database providers with default-provider support
- `bunorm.ManagerRegistry` — caches and manages `*bunorm.Manager` instances (1:1 per provider definition); exposes `Manager(name)` / `MustManager(name)` / `DefaultManager()` / `MustDefaultManager()` / `DefaultDatabase()` / `MustDefaultDatabase()` accessors
- `bunorm.Manager` — owns a single `*bun.DB`; exposes `Database()` and `Close()` methods
- Error sentinels: `ErrResolverIsRequired`, `ErrNoProviderDefinitions`, `ErrProviderDefinitionNameIsRequired`, `ErrProviderIsRequired`, `ErrProviderDefinitionNameMustBeUnique`, `ErrMultipleDefaultProviderDefinitions`
- README with service registration patterns

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.0...integrations/bunorm/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v2.0.0...integrations/bunorm/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v1.0.0...integrations/bunorm/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fv1.0.0
