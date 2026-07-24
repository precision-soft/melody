# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.0.7] - 2026-07-25 - Connection-Abort Retry Markers

### Fixed

- `provider.go` — transient-error detection recognises a connection abort through explicit markers for both spellings its platforms give it (`software caused connection abort` and `established connection was aborted`), and the deprecated `net.Error.Temporary()` call is removed: the interface was deprecated in Go 1.18 for its ill-defined semantics, and the one retryable case it uniquely caught is now covered by the marker

## [v2.0.6] - 2026-07-24 - Cold-Start Transient Markers

### Fixed

- the connection retry treats any `the database system is ...` cold-start FATAL (starting up, in recovery mode, shutting down) as transient, so a server still replaying WAL is retried instead of failing the open on the first attempt

## [v2.0.5] - 2026-07-17 - Connection-Retry Backoff Clamps

### Fixed

- `provider.go` — the connection retry backoff rejects degenerate values: a non-positive `InitialDelay` or `MaxDelay` and a `BackoffMultiplier` that is not at least 1 — a NaN included, which fails every comparison and so slipped through a below-1 check, poisoned the float-space growth and converted to a negative duration — fall back to the defaults instead of collapsing every retry into an immediate re-dial (a negative delay made the sleep return immediately; a sub-1 multiplier decayed the delay toward zero). A multiplier of exactly 1 remains a valid constant backoff.

## [v2.0.4] - 2026-07-11 - TLS Verification Hardening

### Fixed

- `provider.go` — the default connection now VERIFIES the server certificate. With no explicit `WithTlsConfig`, the provider called `pgdriver.WithInsecure(false)`, which — despite the name — pgdriver implements as `tls.Config{InsecureSkipVerify: true}`: TLS was negotiated but the certificate was never checked, so the default connection was trivially machine-in-the-middled. It now builds a verifying config (system roots, the configured host as `ServerName`, TLS 1.2 floor). Connectivity is unchanged for everyone who was connecting successfully: the old default already required SSL on the server, and `WithInsecure(true)` still selects a plaintext session. Deployments using a self-signed certificate must now pass it via `WithTlsConfig`.

## [v2.0.3] - 2026-07-06 - Connection-Retry Backoff Overflow and Transient-Error Coverage

### Fixed

- `provider.go` — the connection-retry backoff no longer collapses to zero at very high attempt counts: `computeBackoffDelay` now grows the delay in float space and returns `MaxDelay` as soon as it is reached, instead of converting `float64(initialDelay) * multiplier` to `time.Duration` first — for an aggressive `RetryConfig` (e.g. `MaxAttempts >= 37` with the default 2× multiplier) the product overflowed `int64` to a negative duration that slipped past the `> MaxDelay` cap, so `time.Sleep` returned immediately and late attempts re-dialled with no backoff.
- `provider.go` — `isTransientError` now also treats bare `EOF`/`unexpected EOF`, `use of closed network connection`, `connection closed`, and `connection reset` as transient, so a graceful peer close during the handshake phase of a cold-starting database is retried instead of hard-failing on the first attempt.

## [v2.0.2] - 2026-06-25 - Guard openWithRetry Against a Nil Logger

### Fixed

- `provider.go` — `openWithRetry` called `logger.Info`/`Warning`/`Error` directly, so a direct `Provider.Open(params, nil)` call (a nil logger) with a retry config and a transient open failure panicked with a nil-pointer dereference. It now normalizes the logger via `logging.EnsureLogger`, matching the `v1`/`v3` providers.

## [v2.0.1] - 2026-06-16 - Honor Zero ConnectTimeout on the Connection Ping

### Fixed

- `v2/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a healthy database. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, back-porting the `v3` fix.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Simplify Provider.Open Signature

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v2` — Go v2 migration
- `v2/provider.go` — `Provider.Open()` signature changed to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `NewProvider()` refactored to accept `...ProviderOption` variadic args only

### Changed

- Code moved to `integrations/bunorm/pgsql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.7...HEAD

[v2.0.7]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.6...integrations/bunorm/pgsql/v2.0.7

[v2.0.6]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.5...integrations/bunorm/pgsql/v2.0.6

[v2.0.5]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.4...integrations/bunorm/pgsql/v2.0.5

[v2.0.4]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.3...integrations/bunorm/pgsql/v2.0.4

[v2.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.2...integrations/bunorm/pgsql/v2.0.3

[v2.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.1...integrations/bunorm/pgsql/v2.0.2

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.0...integrations/bunorm/pgsql/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v2.0.0
