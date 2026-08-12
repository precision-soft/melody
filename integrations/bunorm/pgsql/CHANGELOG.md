# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Provider.OpenForMigration` — the provider implements `bunorm.MigrationProvider`, the door its mysql sibling already had: the same database with the driver deadlines lifted, a two-connection pool and no mid-run recycling. Without it the migrate commands fell back to the request pool, where pgdriver's own default read deadline — 10 seconds, configured by nothing in this package — cut any DDL statement that legitimately ran past it, measured with an 11-second statement dying at 10.004s with an i/o timeout
- `Provider.OpenContext` — the provider implements `bunorm.ContextOpener`: the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget

### Changed

- README — the timeout section describes the three-deadline `TimeoutConfig` that ships: it still claimed the connect timeout was the only field "because pgdriver exposes no separate read/write deadlines", a rationale the package's own code states the opposite of, and its defaults table lacked the 30s read and write rows
- `TimeoutConfig` names every deadline the driver applies — `ReadTimeout` and `WriteTimeout` join `ConnectTimeout` — and the connector receives all three, the dial included: the configured connect timeout never reached the dial, which ran under pgdriver's internal 5-second default whatever the operator set, and the read/write deadlines governed invisibly at the driver's 10s/5s with no field in this package to even mention they existed. The defaults mirror the mysql sibling at 30s/30s. **Breaking** at compile time: `NewTimeoutConfig` takes the three durations, the mysql signature. **Behavioural change**: the effective read and write deadlines move from the driver's invisible 10s/5s to the documented 30s/30s
- `Provider.Open` marks the configured password parameter secret through the configuration's own `MarkSecret` — the component told authoritatively which parameter holds the credential arms the framework's redaction for it
- the terminal failure record of the retry loop is the log of that failure: written in full through the exception context, and the returned error carries the already-logged mark, so the exit handler no longer writes the same outage a second time

### Fixed

- `provider.go` — the caller's own cancellation is a clean stop, recorded at warning under its own name and not retried. The transient classifier reads error types and message markers, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database — the fifth site of a class the framework's fourth pass classified at four others, and the one that this module's own early refusal created. A cancellation that lands while an attempt waits out its backoff is recorded and marked the same way, so it does not travel up as a bare resolution failure for some later writer to file at error. Only `context.Canceled`: the ping budget derives from the connect timeout, so a deadline here can be the database itself

- `provider.go` — the pool half of the migration derivation survives its own normalization: `resolvedPoolConfig` keeps the deliberately-lifted `ConnectionMaxLifetime`/`ConnectionMaxIdleTime` zeros of a migration-tuned provider, the guard `resolvedTimeoutConfig` already carried for the deadlines and the mysql sibling carried for both halves. Re-armed to the request defaults, the dedicated migration connections were recycled five minutes into a run — a lifetime rotation under a running statement being the same cut `OpenForMigration` names as the reason it exists
- `provider.go` — every non-positive field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default, the way the retry configuration already did. A zero reaches the provider far more often from an environment key nobody set than from a caller who means "no limit": on `database/sql` a zero maximum is an UNLIMITED pool and a zero lifetime means connections that are never recycled, and a non-positive connect timeout left the startup ping with no deadline of its own. **Behavioural**: a configuration that relied on zero meaning "unlimited" now receives the documented defaults
- `IsDuplicateKey` answers on the typed SQLSTATE — `23505`, matched through `errors.As`, which sees through wrapping — instead of probing the rendered message for a substring. The probe answered true for any error whose text merely contained the digits (a quoted value in a CHECK-violation detail was enough) and answered false for a real duplicate-key error wrapped in an exception whose message hides its cause. **Behavioural**: errors that carry no PostgreSQL protocol error — a hand-written message containing "duplicate key", an error stringified across a process boundary — no longer match

- `provider.go` — transient-error detection recognises a connection abort through explicit markers for both spellings its platforms give it (`software caused connection abort` and `established connection was aborted`), and the deprecated `net.Error.Temporary()` call is removed: the interface was deprecated in Go 1.18 for its ill-defined semantics, and the one retryable case it uniquely caught is now covered by the marker

## [v1.1.6] - 2026-07-24 - Cold-Start Transient Markers

### Fixed

- the connection retry treats any `the database system is ...` cold-start FATAL (starting up, in recovery mode, shutting down) as transient, so a server still replaying WAL is retried instead of failing the open on the first attempt

## [v1.1.5] - 2026-07-17 - Connection-Retry Backoff Clamps

### Fixed

- `provider.go` — the connection retry backoff rejects degenerate values: a non-positive `InitialDelay` or `MaxDelay` and a `BackoffMultiplier` that is not at least 1 — a NaN included, which fails every comparison and so slipped through a below-1 check, poisoned the float-space growth and converted to a negative duration — fall back to the defaults instead of collapsing every retry into an immediate re-dial (a negative delay made the sleep return immediately; a sub-1 multiplier decayed the delay toward zero). A multiplier of exactly 1 remains a valid constant backoff.

## [v1.1.4] - 2026-07-11 - TLS Verification Hardening

### Fixed

- `provider.go` — the default connection now VERIFIES the server certificate. With no explicit `WithTlsConfig`, the provider called `pgdriver.WithInsecure(false)`, which — despite the name — pgdriver implements as `tls.Config{InsecureSkipVerify: true}`: TLS was negotiated but the certificate was never checked, so the default connection was trivially machine-in-the-middled. It now builds a verifying config (system roots, the configured host as `ServerName`, TLS 1.2 floor). Connectivity is unchanged for everyone who was connecting successfully: the old default already required SSL on the server, and `WithInsecure(true)` still selects a plaintext session. Deployments using a self-signed certificate must now pass it via `WithTlsConfig`.

## [v1.1.3] - 2026-07-06 - Connection-Retry Backoff Overflow and Transient-Error Coverage

### Fixed

- `provider.go` — the connection-retry backoff no longer collapses to zero at very high attempt counts: `computeBackoffDelay` now grows the delay in float space and returns `MaxDelay` as soon as it is reached, instead of converting `float64(initialDelay) * multiplier` to `time.Duration` first — for an aggressive `RetryConfig` (e.g. `MaxAttempts >= 37` with the default 2× multiplier) the product overflowed `int64` to a negative duration that slipped past the `> MaxDelay` cap, so `time.Sleep` returned immediately and late attempts re-dialled with no backoff.
- `provider.go` — `isTransientError` now also treats bare `EOF`/`unexpected EOF`, `use of closed network connection`, `connection closed`, and `connection reset` as transient, so a graceful peer close during the handshake phase of a cold-starting database is retried instead of hard-failing on the first attempt.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.6...HEAD

[v1.1.6]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.5...integrations/bunorm/pgsql/v1.1.6

[v1.1.5]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.4...integrations/bunorm/pgsql/v1.1.5

[v1.1.4]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.3...integrations/bunorm/pgsql/v1.1.4

[v1.1.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.2...integrations/bunorm/pgsql/v1.1.3

[v1.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.1...integrations/bunorm/pgsql/v1.1.2

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.1.0...integrations/bunorm/pgsql/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.2...integrations/bunorm/pgsql/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.1...integrations/bunorm/pgsql/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v1.0.0...integrations/bunorm/pgsql/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v1.0.0
