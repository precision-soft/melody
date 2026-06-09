# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.0] - 2026-06-09 - MySQL Advisory Lock (GET_LOCK)

### Added

- `v3/service_resolver.go` — `RegisterLockerService(registrar, database)` registers the MySQL `GET_LOCK` locker under the core `lock.ServiceLocker`, so userland wires it into the container in one call.
- `v3/lock.go` — MySQL `GET_LOCK`-backed implementation of the core `lock/contract.Locker`/`Lock`. `NewLocker(database)` creates named locks; `Acquire` is non-blocking (`GET_LOCK(?, 0)`, consistent with the try-acquire semantics of the in-memory and Redis backends) and takes a session-scoped lock on a dedicated `*sql.Conn` that is pinned for the lifetime of the held lock and released (`RELEASE_LOCK`) on the same connection. MySQL advisory locks are connection-lifetime: they do not auto-expire, so the `CreateLock(name, ttl)` `ttl` is accepted only for interface compatibility and is documented as not honored as an expiry; `Refresh` therefore has nothing to extend but verifies the lock is still held on its connection (`IS_USED_LOCK(?) = CONNECTION_ID()`) and returns a "lock is no longer held" error if it has been lost, matching the lost-lock signal of the in-memory and Redis backends.

### Fixed

- `v3/lock.go` — when `Refresh` detects the lock was lost (the owning session was killed or the lock forcibly released) or its probe query errors, it now closes and clears the pinned `*sql.Conn` before returning the error. Previously the connection was left set, so the next `Acquire` took the "already held" fast path (`nil != instance.connection`) and falsely reported the lock as held without re-issuing `GET_LOCK`, breaking mutual exclusion after a lock-loss event.
- `v3/lock.go` — a reentrant `Acquire` on a still-set connection now re-verifies ownership (`IS_USED_LOCK(?) = CONNECTION_ID()`) before taking the fast path, and transparently re-acquires on a fresh connection if the pinned one was dropped. Previously a reentrant `Acquire` made *without* an intervening `Refresh` returned `(true, nil)` purely because `instance.connection` was non-nil, so if that connection had died (and MySQL had already auto-released the lock) the original holder and a competitor that grabbed the freed lock could both believe they held it at once.
- `v3/lock.go` — the reentrant-verify error paths of `Acquire` and `Refresh` now best-effort `RELEASE_LOCK` before closing the pinned `*sql.Conn`, so a still-held named lock is no longer orphaned in the pool. Closing a `*sql.Conn` returns the underlying MySQL session to the pool **without** releasing its session-scoped `GET_LOCK` (the driver's session reset does not run `RELEASE_ALL_LOCKS`), so when the ownership probe failed for a reason other than a dead session — for example the runtime context was already cancelled or the probe hit a transient/timed-out query while the session was alive and still owned the lock — the lock stayed held by the pooled session with nothing referencing it, blocking every subsequent acquirer of that name until the connection was recycled (up to `ConnMaxLifetime`). The release runs on a short bounded background context so a cancelled request context cannot prevent the cleanup, mirroring the `RELEASE_LOCK`-before-`Close` that the normal `Release` path already performs.
- `v3/provider.go` — `open` now treats a zero `ConnectTimeout` as "no deadline" on the connectivity ping, matching the guard already applied to the post-build hook a few lines above. The ping wrapped the context with `context.WithTimeout(ctx, 0)` unconditionally, and a zero duration yields an already-expired deadline, so a provider built with `NewTimeoutConfig(0, …)` failed every `Open` with `database connection failed` against a fully reachable database. The two code paths now interpret a zero `ConnectTimeout` identically.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.2...integrations/bunorm/mysql/v3.1.0

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.1...integrations/bunorm/mysql/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.0...integrations/bunorm/mysql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v2.0.0

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.1.0...integrations/bunorm/mysql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.1...integrations/bunorm/mysql/v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v1.0.0...integrations/bunorm/mysql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v1.0.0
