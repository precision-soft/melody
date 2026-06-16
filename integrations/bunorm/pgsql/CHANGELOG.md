# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.1.2] - 2026-06-16 - Honor Zero ConnectTimeout on the Connection Ping

### Fixed

- `provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a healthy database. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, back-porting the `v3` fix.

## [v1.1.1] - 2026-02-17 - Fix Transient Error Detection for DNS Errors

### Fixed

- `provider.go` — `isTransientError()` now walks wrapped errors via `errors.Unwrap()` loop instead of inspecting only the top-level message
- `provider.go` — added detection for `*net.DNSError` so DNS-related transient failures trigger a retry

## [v1.1.0] - 2026-02-17 - Add PostBuildHook and ProviderOption Infrastructure

### Changed

- `provider.go` — `NewProvider()` and `NewProviderWithConfig()` now accept `...ProviderOption` variadic parameters
- `pool_config.go` — `PoolConfig` updated with additional timeout fields
- `README.md` — expanded with post-build hook pattern

### Added

- `post_build_hook.go` — `pgsql.PostBuildHook` function type for post-connector customization (e.g., TLS customization)
- `provider_option.go` — `pgsql.ProviderOption` builder type; `pgsql.WithPostBuildHook(hook)` option constructor
- `dialect.go` — `dialectWithDefaultSchema` extracted into its own file
- `pgsql_error.go` — renamed from `mysql_error.go`; PostgreSQL-specific error detection utilities
- `retry_config.go` — `RetryConfig` and `DefaultRetryConfig()` extracted into dedicated file

### Removed

- `mysql_error.go` — replaced with correctly named `pgsql_error.go`

## [v1.0.2] - 2026-02-16 - Add IsDuplicateKey Helper

### Added

- `mysql_error.go` (renamed to `pgsql_error.go` in v1.1.0) — `IsDuplicateKey(err)` helper for detecting PostgreSQL duplicate-key violations

## [v1.0.1] - 2026-02-07 - Add Retry Mechanism with Exponential Backoff

### Changed

- `provider.go` — `Provider.Open()` delegates to retry logic when retry config is present

### Added

- `provider.go` — `Provider.openWithRetry()` implementing exponential backoff; `computeBackoffDelay()`; `isTransientError()` detecting connection-refused / I/O-timeout patterns
- Retry configuration: `RetryConfig` with `MaxAttempts`, `InitialDelay`, `MaxDelay`, `BackoffMultiplier`; `DefaultRetryConfig()` — 3 attempts, 500ms initial delay, 5s max delay, 2.0× backoff multiplier

## [v1.0.0] - 2026-02-05 - Initial Release — PostgreSQL Provider for bunorm

### Added

- `provider.go` — `pgsql.Provider` implementing `bunorm.Provider`; opens `*bun.DB` via `pgdriver` + `pgdialect`; `pgsql.NewProvider(hostParamName, portParamName, databaseParamName, userParamName, passwordParamName)` constructor; `NewProviderWithConfig()` variant accepting pre-built `PoolConfig` and `TimeoutConfig`
- `pool_config.go` — `pgsql.PoolConfig` with `MaxOpenConnections`, `MaxIdleConnections`, `ConnectionMaxLifetime`, `ConnectionMaxIdleTime`
- `timeout_config.go` — `pgsql.TimeoutConfig` with `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `connection_config.go` — `pgsql.ConnectionConfig` holding connection details; `SafeContext()` excludes password from logs
- Builder methods: `Provider.WithPoolConfig()`, `WithTimeoutConfig()`

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.2...HEAD

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.1...integrations/bunorm/pgsql/v1.1.2

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.0...integrations/bunorm/pgsql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.2...integrations/bunorm/pgsql/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.1...integrations/bunorm/pgsql/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.0...integrations/bunorm/pgsql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v1.0.0
