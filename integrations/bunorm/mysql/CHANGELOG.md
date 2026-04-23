# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.2] - 2026-04-20 - Drop Deprecated net.Error.Temporary Probe

### Changed

- `provider.go` — removed deprecated `net.Error.Temporary()` call from transient-error detection (the `Temporary()` interface was deprecated in Go 1.18). Transient detection now relies on `errors.Is`/`errors.As` and string-pattern matching for connection-refused / I/O-timeout conditions.

## [v3.0.1] - 2026-03-08 - Tidy v2 and v3 go.sum Dependencies

### Changed

- `v2/go.sum`, `v3/go.sum` — resolved transitive dependency checksums; no logic changes
- `v2/provider.go`, `v3/provider.go` — no API changes (module tidy only)

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/mysql/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/mysql/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `bunorm/v3` and `melody/v3`

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Simplify Provider.Open Signature

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/mysql/v2` — Go v2 migration
- `v2/provider.go` — `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `v2/provider.go` — `NewProvider()` no longer accepts parameter names; takes `...ProviderOption` variadic args instead
- `v2/provider.go` — removed builder methods `WithPoolConfig()`, `WithTimeoutConfig()`, `WithRetryConfig()` — options now supplied through `ProviderOption`

### Changed

- Code moved to `integrations/bunorm/mysql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

## [v1.1.1] - 2026-02-17 - Fix Transient Error Detection for DNS Errors

### Fixed

- `provider.go` — `isTransientError()` now walks wrapped errors via `errors.Unwrap()` loop instead of inspecting only the top-level message
- `provider.go` — added detection for `*net.DNSError` so DNS-related transient failures trigger a retry

## [v1.1.0] - 2026-02-17 - Add PostBuildHook and ProviderOption Infrastructure

### Fixed

- `provider.go` — connection error handling improved to support hook-based TLS customization

### Changed

- `provider.go` — `NewProvider()` and `NewProviderWithConfig()` now accept `...ProviderOption` variadic parameters
- `provider.go` — removed exported `DefaultRetryConfig()` — retry config still configurable through `ProviderOption`
- `README.md` — expanded with advanced connector customization examples

### Added

- `post_build_hook.go` — `mysql.PostBuildHook` function type: `func(ctx context.Context, resolver containercontract.Resolver, driverConfig *driver.Config) error`; runs after defaults and typed configs, before SQL connector creation (enables `TLSConfig` mutation and other driver-level customization)
- `provider_option.go` — `mysql.ProviderOption` builder type; `mysql.WithPostBuildHook(hook)` option constructor
- `retry_config.go` — `RetryConfig` extracted into dedicated file

## [v1.0.1] - 2026-02-07 - Add Retry Mechanism with Exponential Backoff

### Changed

- `provider.go` — `Provider.Open()` delegates to `openWithRetry()` when retry config is present

### Added

- `provider.go` — `Provider.openWithRetry()` implementing exponential backoff; `computeBackoffDelay()`; `isTransientError()` detecting connection-refused / I/O-timeout patterns
- Retry configuration: `RetryConfig` with `MaxAttempts`, `InitialDelay`, `MaxDelay`, `BackoffMultiplier`; `DefaultRetryConfig()` — 3 attempts, 500ms initial delay, 5s max delay, 2.0× backoff multiplier

## [v1.0.0] - 2026-02-05 - Initial Release — MySQL Provider for bunorm

### Added

- `provider.go` — `mysql.Provider` implementing `bunorm.Provider`; opens `*bun.DB` via `go-sql-driver/mysql` + `mysqldialect`; `mysql.NewProvider(hostParamName, portParamName, databaseParamName, userParamName, passwordParamName)` constructor; `NewProviderWithConfig()` variant accepting pre-built `PoolConfig` and `TimeoutConfig`
- `pool_config.go` — `mysql.PoolConfig` with `MaxOpenConnections`, `MaxIdleConnections`, `ConnectionMaxLifetime`, `ConnectionMaxIdleTime`
- `timeout_config.go` — `mysql.TimeoutConfig` with `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `connection_config.go` — `mysql.ConnectionConfig` holding connection details; `SafeContext()` excludes password from logs
- Builder methods: `Provider.WithPoolConfig()`, `WithTimeoutConfig()`
- `mysql_error.go` — MySQL-specific error detection utilities

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.2...HEAD

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.1...integrations/bunorm/mysql/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.0...integrations/bunorm/mysql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v2.0.0...integrations/bunorm/mysql/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.1...integrations/bunorm/mysql/v2.0.0

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fmysql%2Fv1.0.0
