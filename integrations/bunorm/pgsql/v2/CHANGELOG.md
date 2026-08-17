# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- documentation: the readme states that a supplied `PoolConfig` or `TimeoutConfig` has every non-positive field replaced by the default, the same field-by-field fill it presented as `RetryConfig`'s exception. `WithInsecure(true)` is described as disabling TLS entirely rather than restoring a legacy plain-TCP default: what preceded the verifying handshake was `pgdriver`'s own insecure mode, which negotiates TLS with verification off. The post-build-hook example guards the `TLSConfig` it dereferences, which is nil under the very option documented two sections above it

- the zero-connect-timeout test message stops claiming a deadline-free dial: a non-positive `ConnectTimeout` is resolved to the default connect deadline before it reaches the driver — the documented normalization — and the failure message now says so instead of describing a no-deadline semantics the provider refuses to have

- the provider's GoDoc stops claiming a construction-time server query: both open-path comments said the dialect handshake bun performs at construction queries the server under no caller context — true of the mysql twin the sentence was copied from, false here, since pgdialect's `Init` has an empty body. They now say what pgsql pays: no construction-time round trip, the first packet on the wire being the boot ping's dial, made under the caller's context and bounded by the connect timeout
- the readme's retry fill-in rule states the multiplier's real floor: it claimed every listed value fills in when the supplied field is zero or non-positive, while for `BackoffMultiplier` the code replaces any supplied value below `1`, `NaN` included, with the default `2.0`, and keeps exactly `1` as a valid constant backoff — so a configured `0.5` was documented as honoured and was not
- bun's diagnostics are routed into the journal on the retry-less open path too: the `bunorm.RouteDiagnostics` call sat only in the retry loop, so the default provider — one built without a `RetryConfig` — opened its connection and left bun's declaration mistakes as unstructured lines on standard error, exactly the state the routing was added to end. The call now lives in the one open funnel every door shares — `Open`, `OpenContext`, the retry loop and the migration door alike

### Changed

- the `bun` requirement moves to `v1.2.17`, with the dialect and driver packages in lockstep: the dialects verify at init that their version equals bun's and panic otherwise. v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over a schema that never changed; the whole family moves together so no binary can assemble a mismatched pair

### Added

- opening a connection routes bun's own diagnostic channel into the application's journal, once per process, through `bunorm.RouteDiagnostics`: bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — arrive as warning records instead of unstructured lines on standard error. See the `bunorm` changelog for the door itself. The pgsql dialect, unlike the mysql one, writes nothing through the standard library's logger, so this covers everything bun reports here

- `Provider.OpenForMigrationContext` — the provider implements `bunorm.MigrationContextOpener`: the migration open runs under the caller's context, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. `OpenForMigration` is the same call under `context.Background()`, so no existing call site changes

- `Provider.OpenForMigration` — the provider implements `bunorm.MigrationProvider`, the door its mysql sibling already had: the same database with the driver deadlines lifted, a two-connection pool and no mid-run recycling. Without it the migrate commands fell back to the request pool, where pgdriver's own default read deadline — 10 seconds, configured by nothing in this package — cut any DDL statement that legitimately ran past it, measured with an 11-second statement dying at 10.004s with an i/o timeout
- `Provider.OpenContext` — the provider implements `bunorm.ContextOpener`: the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget

### Changed

- README — the timeout section describes the three-deadline `TimeoutConfig` that ships: it still claimed the connect timeout was the only field "because pgdriver exposes no separate read/write deadlines", a rationale the package's own code states the opposite of, and its defaults table lacked the 30s read and write rows
- `TimeoutConfig` names every deadline the driver applies — `ReadTimeout` and `WriteTimeout` join `ConnectTimeout` — and the connector receives all three, the dial included: the configured connect timeout never reached the dial, which ran under pgdriver's internal 5-second default whatever the operator set, and the read/write deadlines governed invisibly at the driver's 10s/5s with no field in this package to even mention they existed. The defaults mirror the mysql sibling at 30s/30s. **Breaking** at compile time: `NewTimeoutConfig` takes the three durations, the mysql signature. **Behavioural change**: the effective read and write deadlines move from the driver's invisible 10s/5s to the documented 30s/30s
- credential redaction is the application's call on this major, not the provider's: v1's `Provider.Open` marks its configured password parameter through the configuration's own `MarkSecret`, but this major hands the provider the connection VALUES rather than the parameter names it would read them under, so the provider knows no configuration key. The equivalent door is `bunorm.ManagerRegistry.MarkSecretParameters(configuration, names...)`, called by the party that resolved the values; the provider carries no `bunorm.SecretParameterProvider`
- the terminal failure record of the retry loop is the log of that failure: written in full through the exception context, and the returned error carries the already-logged mark, so the exit handler no longer writes the same outage a second time

### Fixed

- `provider.go` — the retry warning carries the diagnostic shape the terminal records carry: the host and port dialed, the pool sizing, the deadlines that governed the attempt and the cause chain, lifted through `exception.LogContext` the way the three records above it already are. The first two records an operator sees when a database is down had the failure flattened to `openErr.Error()` — a message and nothing to act on — and only the third, terminal one named the database

- `provider.go` — the caller's own cancellation is a clean stop, recorded at warning under its own name and not retried. The transient classifier reads error types and message markers, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database — the fifth site of a class the framework's fourth pass classified at four others, and the one that this module's own early refusal created. A cancellation that lands while an attempt waits out its backoff is recorded and marked the same way, so it does not travel up as a bare resolution failure for some later writer to file at error. Only `context.Canceled`: the ping budget derives from the connect timeout, so a deadline here can be the database itself

- `provider.go` — the pool half of the migration derivation survives its own normalization: `resolvedPoolConfig` keeps the deliberately-lifted `ConnectionMaxLifetime`/`ConnectionMaxIdleTime` zeros of a migration-tuned provider, the guard `resolvedTimeoutConfig` already carried for the deadlines and the mysql sibling carried for both halves. Re-armed to the request defaults, the dedicated migration connections were recycled five minutes into a run — a lifetime rotation under a running statement being the same cut `OpenForMigration` names as the reason it exists
- `provider.go` — every non-positive field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default, the way the retry configuration already did. A zero reaches the provider far more often from an environment key nobody set than from a caller who means "no limit": on `database/sql` a zero maximum is an UNLIMITED pool and a zero lifetime means connections that are never recycled, and a non-positive connect timeout left the startup ping with no deadline of its own. **Behavioural**: a configuration that relied on zero meaning "unlimited" now receives the documented defaults
- `IsDuplicateKey` answers on the typed SQLSTATE — `23505`, matched through `errors.As`, which sees through wrapping — instead of probing the rendered message for a substring. The probe answered true for any error whose text merely contained the digits (a quoted value in a CHECK-violation detail was enough) and answered false for a real duplicate-key error wrapped in an exception whose message hides its cause. **Behavioural**: errors that carry no PostgreSQL protocol error — a hand-written message containing "duplicate key", an error stringified across a process boundary — no longer match

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.6...HEAD

[v2.0.6]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.5...integrations/bunorm/pgsql/v2.0.6

[v2.0.5]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.4...integrations/bunorm/pgsql/v2.0.5

[v2.0.4]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.3...integrations/bunorm/pgsql/v2.0.4

[v2.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.2...integrations/bunorm/pgsql/v2.0.3

[v2.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.1...integrations/bunorm/pgsql/v2.0.2

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v2.0.0...integrations/bunorm/pgsql/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v2.0.0
