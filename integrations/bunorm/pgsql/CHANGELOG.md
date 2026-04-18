# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.1] - 2026-03-08

### Changed

- Patch release — `v2/go.sum` and `v3/go.sum` updated with resolved transitive dependencies; no API changes

## [v3.0.0] - 2026-03-08

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/pgsql/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `bunorm/v3` and `melody/v3`

## [v2.0.0] - 2026-02-17

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v2` — Go v2 migration
- `Provider.Open()` signature changed to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `NewProvider()` refactored to accept `...ProviderOption` variadic args only

### Changed

- Code moved to `integrations/bunorm/pgsql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

## [v1.1.1] - 2026-02-17

### Fixed

- `Provider.isTransientError()` now walks wrapped errors via `errors.Unwrap()` loop instead of inspecting only the top-level message
- Added detection for `*net.DNSError` so DNS-related transient failures trigger a retry

## [v1.1.0] - 2026-02-17

### Added

- `pgsql.PostBuildHook` — function type for post-connector customization (e.g., TLS customization)
- `pgsql.ProviderOption` builder type
- `pgsql.WithPostBuildHook(hook)` option constructor
- `dialect.go` — `dialectWithDefaultSchema` extracted into its own file
- `pgsql_error.go` — renamed from `mysql_error.go`; PostgreSQL-specific error detection utilities
- `retry_config.go` — `RetryConfig` and `DefaultRetryConfig()` extracted into dedicated file

### Changed

- `NewProvider()` and `NewProviderWithConfig()` now accept `...ProviderOption` variadic parameters
- `PoolConfig` updated with additional timeout fields
- README expanded with post-build hook pattern

### Removed

- `mysql_error.go` (replaced with correctly named `pgsql_error.go`)

## [v1.0.2] - 2026-02-16

### Fixed

- Retry error-detection utilities split out; `go.mod` updated with resolved dependencies

## [v1.0.1] - 2026-02-07

### Added

- Retry mechanism with `pgsql.RetryConfig` (`MaxAttempts`, `InitialDelay`, `MaxDelay`, `BackoffMultiplier`)
- `Provider.WithRetryConfig()` builder method
- `DefaultRetryConfig()` returning 3 attempts / 500ms initial / 5s max / 2.0× backoff
- `Provider.openWithRetry()`, `computeBackoffDelay()`, `isTransientError()` private methods

### Changed

- `Provider.Open()` delegates to retry logic when retry config is present

## [v1.0.0] - 2026-02-05

### Added

- Initial release — PostgreSQL provider for `bunorm`
- `pgsql.Provider` implementing `bunorm.Provider`; opens `*bun.DB` via `pgdriver` + `pgdialect`
- `pgsql.NewProvider(hostParamName, portParamName, databaseParamName, userParamName, passwordParamName)` constructor
- `pgsql.NewProviderWithConfig()` variant accepting pre-built `PoolConfig` and `TimeoutConfig`
- `pgsql.PoolConfig` — `MaxOpenConnections`, `MaxIdleConnections`, `ConnectionMaxLifetime`, `ConnectionMaxIdleTime`
- `pgsql.TimeoutConfig` — `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `pgsql.ConnectionConfig` — holds connection details; `SafeContext()` excludes password from logs
- `dialectWithDefaultSchema` — wraps `pgdialect.Dialect`, overrides `DefaultSchema()` to return `"public"`
- Builder methods: `Provider.WithPoolConfig()`, `WithTimeoutConfig()`

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.0...integrations/bunorm/pgsql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.0...integrations/bunorm/pgsql/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.1...integrations/bunorm/pgsql/v2.0.0

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.0...integrations/bunorm/pgsql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.2...integrations/bunorm/pgsql/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.1...integrations/bunorm/pgsql/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.0...integrations/bunorm/pgsql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fpgsql%2Fv1.0.0
