# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.1] - 2026-03-08

### Changed

- Patch release — `v3/go.mod` and `v3/go.sum` updated with resolved transitive dependencies; no API changes

## [v3.0.0] - 2026-03-08

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/rueidis/v3` — Go v3 migration
- `rueidis.ConnectionConfig` renamed to `ConnectionParams`; now a value type (no longer pointer-based); `NewConnectionParams()` returns a value
- `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (rueidis.Client, error)` to `Open(params ConnectionParams) (rueidis.Client, error)` — provider no longer couples to container/config resolution

### Changed

- Code duplicated into `integrations/rueidis/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `github.com/precision-soft/melody/v3 v3.0.0`

## [v2.0.0] - 2026-02-17

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/rueidis/v2` — Go v2 migration

### Changed

- Code moved to `integrations/rueidis/v2/` with matching module path
- Local `replace` directive removed from `go.mod`; `github.com/precision-soft/melody` pinned to v1.6.3
- Documentation examples reformatted to be copy-paste runnable (wrapped in `main()` functions)
- `Provider.Open()` signature unchanged in v2 (still accepts `containercontract.Resolver`) — contrast with v3 where it changes

## [v1.0.0] - 2026-02-11

### Added

- Initial release — Redis client integration for Melody backed by `github.com/redis/rueidis` v1.0.71
- `rueidis.Provider` — implements Redis connection provider
- `rueidis.NewProvider(addressParamName, userParamName, passwordParamName)` — reads credentials through Melody config
- `rueidis.NewProviderWithConfig()` — variant accepting pre-built `ClientConfig` and `TimeoutConfig`
- `rueidis.ClientConfig` — `MaxConnPoolSize`, `MinIdleConnections`, `ReadBufferSize`, `WriteBufferSize`
- `rueidis.TimeoutConfig` — `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `rueidis.ConnectionConfig` — address, user, password; `SafeContext()` elides password
- Builder methods: `Provider.WithClientConfig()`, `WithTimeoutConfig()`
- `rueidis/cache/` subpackage:
  - `cache.Backend` — wrapper around `rueidis.Client` with convenience methods
  - `cache.Backend.Get()`, `Set()`, `Delete()`, `Exists()`, `ClearByPrefix()`
  - `cache.BackendService` — service wrapper; `WithContext()` binds a backend to a specific context
  - `cache.BackendFromRuntime()` — obtains a backend from the Melody runtime with bound context

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.0.0...integrations/rueidis/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.0...integrations/rueidis/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.0...integrations/rueidis/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Frueidis%2Fv1.0.0
