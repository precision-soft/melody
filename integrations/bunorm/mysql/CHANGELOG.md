# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.2...HEAD

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.1...integrations/bunorm/mysql/v1.1.2

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v1.0.0
