# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `provider.go` — the connection retry backoff rejects degenerate values: a non-positive `InitialDelay` or `MaxDelay` and a `BackoffMultiplier` that is not at least 1 — a NaN included, which fails every comparison and so slipped through a below-1 check, poisoned the float-space growth and converted to a negative duration — fall back to the defaults instead of collapsing every retry into an immediate re-dial (a negative delay made the sleep return immediately; a sub-1 multiplier decayed the delay toward zero). A multiplier of exactly 1 remains a valid constant backoff.

## [v1.1.3] - 2026-07-06 - Connection-Retry Backoff Overflow and Transient-Error Coverage

### Fixed

- `provider.go` — the connection-retry backoff no longer collapses to zero at very high attempt counts: `computeBackoffDelay` now grows the delay in float space and returns `MaxDelay` as soon as it is reached, instead of converting `float64(initialDelay) * multiplier` to `time.Duration` first — for an aggressive `RetryConfig` (e.g. `MaxAttempts >= 37` with the default 2× multiplier) the product overflowed `int64` to a negative duration that slipped past the `> MaxDelay` cap, so `time.Sleep` returned immediately and late attempts re-dialled with no backoff.
- `provider.go` — `isTransientError` now also treats bare `EOF`/`unexpected EOF`, `use of closed network connection`, `connection closed`, and `connection reset` as transient, so a graceful peer close during the RESP/handshake phase of a cold-starting server is retried instead of hard-failing on the first attempt.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.3...HEAD

[v1.1.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.2...integrations/bunorm/mysql/v1.1.3

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.1...integrations/bunorm/mysql/v1.1.2

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v1.0.0
