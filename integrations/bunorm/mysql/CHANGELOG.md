# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Provider.OpenContext` — the provider implements `bunorm.ContextOpener`: the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget, and the cancellation is reported with the last attempt's own failure as context
- `Provider.OpenForMigration` — the provider implements `bunorm.MigrationProvider` and opens the same database with the driver deadlines lifted: `ReadTimeout` and `WriteTimeout` are per-connection settings baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The connect timeout stays armed, the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run — a lifetime rotation under a running statement is the same cut by another name

### Changed

- README — the retry paragraph states what the code does with a zero `ConnectTimeout`: every non-positive value falls back to the 10s default before the connector is built, so the dial, ping and hook always run under a deadline — the claim that zero "leaves both unbounded" described a state `resolvedTimeoutConfig` cannot produce
- `Provider.Open` marks the configured password parameter secret through the configuration's own `MarkSecret` — the component told authoritatively which parameter holds the credential arms the framework's redaction for it, so the introspection output masks the password and every template derived from it without the application repeating the knowledge
- the terminal failure record of the retry loop is the log of that failure: it is written in full through the exception context and the returned error carries the already-logged mark, so the exit handler no longer writes the same outage a second time. An alert counting error records saw one outage twice on the SQL providers and once on redis

### Fixed

- `provider.go` — the caller's own cancellation is a clean stop, recorded at warning under its own name and not retried. The transient classifier reads error types and message markers, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database — the fifth site of a class the framework's fourth pass classified at four others, and the one that this module's own early refusal created. A cancellation that lands while an attempt waits out its backoff is recorded and marked the same way, so it does not travel up as a bare resolution failure for some later writer to file at error. Only `context.Canceled`: the ping budget derives from the connect timeout, so a deadline here can be the database itself

- `provider.go` — the connection-failure record carries the pool sizing and the deadlines that governed the attempt, the pgsql sibling's diagnostic shape; it named only the address that refused, so the same outage read differently depending on which provider reported it
- `provider.go` — every non-positive field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default, the way the retry configuration already did. A zero reaches the provider far more often from an environment key nobody set than from a caller who means "no limit": on `database/sql` a zero maximum is an UNLIMITED pool and a zero lifetime means connections that are never recycled, while on this driver a zero read or write deadline means no deadline at all — so the zero-value configuration disarmed exactly the protections the nil configuration installs, and a negative one put the deadline in the past, failing every dial instantly with an i/o timeout no network event caused. **Behavioural**: a configuration that relied on zero meaning "unlimited" now receives the documented defaults; the migration connection keeps its deliberately lifted deadlines and disabled recycling
- `provider.go` — transient-error detection recognises a connection abort through explicit markers for both spellings its platforms give it (`software caused connection abort` and `established connection was aborted`), aligning with the pgsql provider

## [v1.1.5] - 2026-07-24 - Server Shutdown Transient Marker

### Fixed

- the connection retry treats `Server shutdown in progress` as transient, so a restarting server is retried instead of failing the open on the first attempt

## [v1.1.4] - 2026-07-17 - Connection-Retry Backoff Clamps

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.5...HEAD

[v1.1.5]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.4...integrations/bunorm/mysql/v1.1.5

[v1.1.4]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.3...integrations/bunorm/mysql/v1.1.4

[v1.1.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.2...integrations/bunorm/mysql/v1.1.3

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.1...integrations/bunorm/mysql/v1.1.2

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v1.0.0
