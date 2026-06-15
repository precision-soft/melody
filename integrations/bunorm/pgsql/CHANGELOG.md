# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `v3/provider.go` — the retry/backoff fallbacks in `openWithRetry`/`computeBackoffDelay` now read from `DefaultRetryConfig()` instead of repeating the `3` / `500ms` / `5s` / `2.0` literals inline, so the documented defaults and the zero-value fallbacks cannot drift apart. Behaviour is unchanged.

## [v3.1.1] - 2026-06-15 - Honor Zero ConnectTimeout on the Connection Ping

### Added

- `v3/README.md` — added a v3 module README documenting the option-based `Provider`, the secure-by-default TLS controls (`WithInsecure`/`WithTlsConfig`), the typed pool/timeout/retry configs, and the post-build hook.

### Fixed

- `v3/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout (`WithTimeoutConfig(NewTimeoutConfig(0))`, which the framework treats elsewhere — and in the same function's post-build-hook block — as "no deadline / wait indefinitely") produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a perfectly healthy database, surfacing as `"database connection failed"`. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, mirroring the bunorm `mysql/v3` provider.
- `v3/provider.go` — `openWithRetry` no longer panics when `Open` is called with a `nil` logger and a `RetryConfig`. The retry path called `logger.Info`/`Warning`/`Error` directly, so a transient connection error dereferenced the nil logger; the logger is now normalized through `logging.EnsureLogger`, matching the framework's nil-logger contract that the non-retry path (and the example wiring) already rely on.

## [v3.1.0] - 2026-04-23 - Default TLS Handshake (MEL-161)

### Fixed

- `provider.go` — default TLS handshake is now enabled. The legacy hardcoded `pgdriver.WithInsecure(true)` silently disabled TLS on every Postgres connection; `insecure` now defaults to `false`, so new `NewProvider(...)` callers negotiate TLS out of the box. Operators who still rely on plain-TCP can opt in with `WithInsecure(true)`. This is a **behavioural change**: deployments without a TLS-capable Postgres endpoint must either expose TLS on the server or explicitly pass `WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

### Changed

- `provider.go` — `Open(...)` now builds the `pgdriver` connector from `instance.insecure` / `instance.tlsConfig` instead of hardcoding `pgdriver.WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

### Added

- `provider_option.go` — `WithInsecure(insecure bool) ProviderOption` lets callers toggle the `pgdriver.WithInsecure(...)` flag (default `false`) (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option.go` — `WithTlsConfig(config *tls.Config) ProviderOption` lets callers pass a `*crypto/tls.Config` that is forwarded to `pgdriver.WithTLSConfig(...)`. When a non-nil `tls.Config` is supplied, it takes precedence over `WithInsecure(...)` (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option_test.go` — coverage for default (`insecure=false`, `tlsConfig=nil`), `WithInsecure(true)` override, and `WithTlsConfig(...)` field storage; mirrored in `v2/` and `v3/`

## [v3.0.1] - 2026-03-08 - Tidy v2 and v3 go.sum Dependencies

### Changed

- `v2/go.sum`, `v3/go.sum` — resolved transitive dependency checksums; no API changes
- `v2/provider.go`, `v3/provider.go` — no logic changes (module tidy only)

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/pgsql/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `bunorm/v3` and `melody/v3`

## [v2.0.1] - 2026-06-11 - Honor Zero ConnectTimeout on the Connection Ping

### Fixed

- `v2/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a healthy database. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, back-porting the `v3` fix that shipped in `v3.1.1`.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Simplify Provider.Open Signature

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v2` — Go v2 migration
- `v2/provider.go` — `Provider.Open()` signature changed to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `NewProvider()` refactored to accept `...ProviderOption` variadic args only

### Changed

- Code moved to `integrations/bunorm/pgsql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

## [v1.1.2] - 2026-06-11 - Honor Zero ConnectTimeout on the Connection Ping

### Fixed

- `provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a healthy database. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, back-porting the `v3` fix that shipped in `v3.1.1`.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.1...HEAD

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.0...integrations/bunorm/pgsql/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.1...integrations/bunorm/pgsql/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.0...integrations/bunorm/pgsql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v3.0.0

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.0...integrations/bunorm/pgsql/v2.0.1
[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v2.0.0

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.1...integrations/bunorm/pgsql/v1.1.2
[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.0...integrations/bunorm/pgsql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.2...integrations/bunorm/pgsql/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.1...integrations/bunorm/pgsql/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.0...integrations/bunorm/pgsql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v1.0.0
