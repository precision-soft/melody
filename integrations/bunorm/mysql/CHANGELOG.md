# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.1] - 2026-03-08

### Changed

- Patch release — `v2/go.sum` and `v3/go.sum` updated with resolved transitive dependencies; no API changes

## [v3.0.0] - 2026-03-08

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/mysql/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/mysql/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `bunorm/v3` and `melody/v3`

## [v2.0.0] - 2026-02-17

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/mysql/v2` — Go v2 migration
- `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `NewProvider()` no longer accepts parameter names; takes `...ProviderOption` variadic args instead
- Removed builder methods `WithPoolConfig()`, `WithTimeoutConfig()`, `WithRetryConfig()` — options now supplied through `ProviderOption`

### Changed

- Code moved to `integrations/bunorm/mysql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

## [v1.1.1] - 2026-02-17

### Fixed

- `Provider.isTransientError()` now walks wrapped errors via `errors.Unwrap()` loop instead of inspecting only the top-level message
- Added detection for `*net.DNSError` so DNS-related transient failures trigger a retry

## [v1.1.0] - 2026-02-17

### Added

- `mysql.PostBuildHook` — `func(ctx context.Context, resolver containercontract.Resolver, driverConfig *driver.Config) error`; runs after defaults and typed configs, before SQL connector creation (enables `TLSConfig` mutation and other driver-level customization)
- `mysql.ProviderOption` builder type
- `mysql.WithPostBuildHook(hook)` option constructor

### Changed

- `NewProvider()` and `NewProviderWithConfig()` now accept `...ProviderOption` variadic parameters
- Removed exported `DefaultRetryConfig()` — retry config still configurable through `ProviderOption`
- README expanded with advanced connector customization examples

### Fixed

- Connection error handling improved to support hook-based TLS customization

## [v1.0.1] - 2026-02-07

### Added

- Retry mechanism with `mysql.RetryConfig` (`MaxAttempts`, `InitialDelay`, `MaxDelay`, `BackoffMultiplier`)
- `Provider.WithRetryConfig()` builder method
- `DefaultRetryConfig()` — 3 attempts, 500ms initial delay, 5s max delay, 2.0× backoff multiplier
- `Provider.openWithRetry()` implementing exponential backoff; `computeBackoffDelay()`; `isTransientError()` detecting connection-refused / I/O-timeout patterns

### Changed

- `Provider.Open()` delegates to `openWithRetry()` when retry config is present

## [v1.0.0] - 2026-02-05

### Added

- Initial release — MySQL provider for `bunorm`
- `mysql.Provider` implementing `bunorm.Provider`; opens `*bun.DB` via `go-sql-driver/mysql` + `mysqldialect`
- `mysql.NewProvider(hostParamName, portParamName, databaseParamName, userParamName, passwordParamName)` constructor
- `mysql.NewProviderWithConfig()` variant accepting pre-built `PoolConfig` and `TimeoutConfig`
- `mysql.PoolConfig` — `MaxOpenConnections`, `MaxIdleConnections`, `ConnectionMaxLifetime`, `ConnectionMaxIdleTime`
- `mysql.TimeoutConfig` — `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `mysql.ConnectionConfig` — holds connection details; `SafeContext()` excludes password from logs
- Builder methods: `Provider.WithPoolConfig()`, `WithTimeoutConfig()`

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.0...integrations/bunorm/mysql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v2.0.0...integrations/bunorm/mysql/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.1...integrations/bunorm/mysql/v2.0.0

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fmysql%2Fv1.0.0
