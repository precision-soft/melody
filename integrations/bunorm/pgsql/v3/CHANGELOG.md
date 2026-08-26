# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- opening a connection routes bun's own diagnostic channel into the application's journal, once per process, through `bunorm.RouteDiagnostics`: bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — arrive as warning records instead of unstructured lines on standard error. See the `bunorm` changelog for the door itself. The pgsql dialect, unlike the mysql one, writes nothing through the standard library's logger, so this covers everything bun reports here

- `Provider.OpenForMigrationContext` — the provider implements `bunorm.MigrationContextOpener`: the migration open runs under the caller's context, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. `OpenForMigration` is the same call under `context.Background()`, so no existing call site changes

- `Provider.OpenForMigration` — the provider implements `bunorm.MigrationProvider`, the door its mysql sibling already had: the same database with the driver deadlines lifted, a two-connection pool and no mid-run recycling. Without it the migrate commands fell back to the request pool, where pgdriver's own default read deadline — 10 seconds, configured by nothing in this package — cut any DDL statement that legitimately ran past it, measured with an 11-second statement dying at 10.004s with an i/o timeout
- `Provider.OpenContext` — the provider implements `bunorm.ContextOpener`: the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget

### Changed

- `TimeoutConfig` names every deadline the driver applies — `ReadTimeout` and `WriteTimeout` join `ConnectTimeout` — and the connector receives all three, the dial included: the configured connect timeout never reached the dial, which ran under pgdriver's internal 5-second default whatever the operator set, and the read/write deadlines governed invisibly at the driver's 10s/5s with no field in this package to even mention they existed. The defaults mirror the mysql sibling at 30s/30s. **Breaking** at compile time: `NewTimeoutConfig` takes the three durations, the mysql signature. **Behavioural change**: the effective read and write deadlines move from the driver's invisible 10s/5s to the documented 30s/30s
- credential redaction is the application's call on this major, not the provider's and not the registry's: v1's `Provider.Open` marks its configured password parameter through the configuration's own `MarkSecret`, but this major hands the provider the connection VALUES rather than the parameter names it would read them under, so the provider knows no configuration key and names no credential. The door is the framework's own parameter registrar — `RegisterSecretParameter` for a parameter the application declares, `MarkParameterSecret` for one melody registered from the `.env` artifacts — called by the party that resolved the values. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it
- the terminal failure record of the retry loop is the log of that failure: written in full through the exception context, and the returned error carries the already-logged mark, so the exit handler no longer writes the same outage a second time

- the `bun` requirement moves to `v1.2.17`, with the dialect and driver packages in lockstep: the dialects verify at init that their version equals bun's and panic otherwise. v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over a schema that never changed; the whole family moves together so no binary can assemble a mismatched pair

### Fixed

- the retry backoff has a FLOOR of one millisecond and is computed in closed form. The configuration guards refused a non-positive delay, which left one NANOSECOND as the smallest thing an operator could ask for — a wait shorter than the dial it separates, which is the re-dial storm those guards exist to prevent arriving through the door they left open. The floor is applied to the initial delay and to the ceiling, so no branch can answer under it. Separately, the growth was walked once per attempt already made — O(attempt) per call and therefore O(attempt²) over a run — and it left the walk early only once the delay had passed the ceiling, which a multiplier of exactly 1, a valid constant backoff, never does; a large attempt budget paid that square in full for a delay that never moved. The growth is now a single exponentiation, capped before the conversion exactly as the walk was, so a large attempt count still cannot overflow into a negative duration. An attempt of zero reads as the first attempt, which is what the walk answered it by never running
- the transient classifier matches its markers as words rather than as bare substrings. The short spellings fired inside ordinary identifiers — `eof` sits inside a table named `geofences`, `timeout` inside a `session_timeout` column — so a permanent failure was retried for the whole budget and then reported as "failed after max retry attempts" rather than as non-transient, costing the delay and telling the operator the wrong thing about what is really a schema error. A boundary is any character that is not a letter, a digit or an underscore, so the spellings carrying spaces and slashes match exactly as before, and the `io.EOF` and `net.Error` checks above the scan are untouched. **Behavioural change**: a failure whose message merely contains a marker inside a word now fails fast instead of exhausting the retry budget — see the framework's `UPGRADE.md`
- a retry cancelled during its backoff hands on the failure it was retrying as structured context rather than as a flattened string. The cause stays the cancellation, because the classification upstream reads it, but the outage arrived as `openErr.Error()` in a single field — the exact flattening the comment eleven lines above condemns for the retry warning, which lifts the failure's own context and its cause chain. The record now carries the host and port dialled, the pool sizing and the deadlines that governed the attempt, so one failure has one record shape whether or not the caller happened to cancel
- the diagnostic of a failed connection names the endpoint the dial actually reached. The connection context is built from the configuration parameters before the post-build hook runs, and the hook may rewrite the address — it is handed the very field for it — so a ping failure named the configured host and port while the attempt had gone somewhere else. The dialled address is carried beside the configured connection rather than folded into it, so a record where the two differ says so
- the negotiated TLS posture is proven where the driver receives it, not only in the helper that computes it. The helper had its own test, but nothing observed that its answer reached the connector, so a deleted assignment would have left every default connection in plaintext with the suite green. The post-build hook is handed the configuration the connector is built from, and three tests read the posture through it: the verifying default with the configured host as the name to verify against, the one plaintext path behind `WithInsecure`, and an explicit `WithTlsConfig` reaching the driver exactly as it was given
- `provider.go` — the retry warning carries the diagnostic shape the terminal records carry: the host and port dialed, the pool sizing, the deadlines that governed the attempt and the cause chain, lifted through `exception.LogContext` the way the three records above it already are. The first two records an operator sees when a database is down had the failure flattened to `openErr.Error()` — a message and nothing to act on — and only the third, terminal one named the database

- `provider.go` — the caller's own cancellation is a clean stop, recorded at warning under its own name and not retried. The transient classifier reads error types and message markers, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database. A cancellation that lands while an attempt waits out its backoff is recorded and marked the same way, so it does not travel up as a bare resolution failure for some later writer to file at error. Only `context.Canceled`: the ping budget derives from the connect timeout, so a deadline here can be the database itself

- `provider.go` — the pool half of the migration derivation survives its own normalization: `resolvedPoolConfig` keeps the deliberately-lifted `ConnectionMaxLifetime`/`ConnectionMaxIdleTime` zeros of a migration-tuned provider, the guard `resolvedTimeoutConfig` already carried for the deadlines and the mysql sibling carried for both halves. Re-armed to the request defaults, the dedicated migration connections were recycled five minutes into a run — a lifetime rotation under a running statement being the same cut `OpenForMigration` names as the reason it exists
- `provider.go` — every non-positive field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default, the way the retry configuration already did. A zero reaches the provider far more often from an environment key nobody set than from a caller who means "no limit": on `database/sql` a zero maximum is an UNLIMITED pool and a zero lifetime means connections that are never recycled, and a non-positive connect timeout left the startup ping with no deadline of its own. **Behavioural**: a configuration that relied on zero meaning "unlimited" now receives the documented defaults
- `IsDuplicateKey` answers on the typed SQLSTATE — `23505`, matched through `errors.As`, which sees through wrapping — instead of probing the rendered message for a substring. The probe answered true for any error whose text merely contained the digits (a quoted value in a CHECK-violation detail was enough) and answered false for a real duplicate-key error wrapped in an exception whose message hides its cause. **Behavioural**: errors that carry no PostgreSQL protocol error — a hand-written message containing "duplicate key", an error stringified across a process boundary — no longer match
- `provider.go` — a nil logger no longer routes the retry loop's records to a discard sink while the terminal error still carries the already-logged mark, which suppressed the framework's own writers too and let a down database fail with zero diagnostic output anywhere; `OpenContext` now reads a nil logger — typed nil included — as the emergency logger, so the retry warnings and the terminal record reach standard error the way the first major always wrote them
- `provider.go` — an open with a nil logger no longer consumes the process-lifetime diagnostics routing with a logger that discards everything, which silenced bun's declaration warnings — unknown struct tag options, queries with arguments but no placeholders — for every later open in the process, the correctly wired ones included; the routing now receives the logger the retry loop logs to, which the nil fallback above guarantees is a sink that actually writes

- `provider.go` — transient-error detection recognises a connection abort through explicit markers for both spellings its platforms give it (`software caused connection abort` and `established connection was aborted`), and the deprecated `net.Error.Temporary()` call is removed: the interface was deprecated in Go 1.18 for its ill-defined semantics, and the one retryable case it uniquely caught is now covered by the marker

## [v3.2.3] - 2026-07-24 - Cold-Start Transient Markers

### Fixed

- the connection retry treats any `the database system is ...` cold-start FATAL (starting up, in recovery mode, shutting down) as transient, so a server still replaying WAL is retried instead of failing the open on the first attempt

## [v3.2.2] - 2026-07-17 - Connection-Retry Backoff Clamps

### Fixed

- `provider.go` — the connection retry backoff rejects degenerate values: a non-positive `InitialDelay` or `MaxDelay` and a `BackoffMultiplier` that is not at least 1 — a NaN included, which fails every comparison and so slipped through a below-1 check, poisoned the float-space growth and converted to a negative duration — fall back to the defaults instead of collapsing every retry into an immediate re-dial (a negative delay made the sleep return immediately; a sub-1 multiplier decayed the delay toward zero). A multiplier of exactly 1 remains a valid constant backoff.

## [v3.2.1] - 2026-07-11 - TLS Verification Hardening

### Fixed

- `provider.go` — the default connection now VERIFIES the server certificate. With no explicit `WithTlsConfig`, the provider called `pgdriver.WithInsecure(false)`, which — despite the name — pgdriver implements as `tls.Config{InsecureSkipVerify: true}`: TLS was negotiated but the certificate was never checked, so the default connection was trivially machine-in-the-middled. It now builds a verifying config (system roots, the configured host as `ServerName`, TLS 1.2 floor). Connectivity is unchanged for everyone who was connecting successfully: the old default already required SSL on the server, and `WithInsecure(true)` still selects a plaintext session. Deployments using a self-signed certificate must now pass it via `WithTlsConfig`.

## [v3.2.0] - 2026-07-06 - PostgreSQL Advisory-Lock Locker

### Added

- `lock.go` — `NewLocker(database, ...)` returns a `lock/contract.Locker` backed by PostgreSQL session advisory locks, the Postgres counterpart of the MySQL `GET_LOCK` locker (which had no pgsql equivalent, blocking applications that use the locker — e.g. ERP WMS delivery-picking and reception-stock-in — from running on Postgres). `Acquire` runs a non-blocking `pg_try_advisory_lock` (try-lock, timeout 0) on a dedicated pinned `*sql.Conn` (a session advisory lock is held by the connection that took it); `Release` runs `pg_advisory_unlock`; and because a session advisory lock has no TTL — it is held for as long as its backend session lives — `Refresh` is a connection-liveness probe (the lock is still held as long as the pinned connection answers, so there is no lease to renew and a transient or canceled request context is never mistaken for a lost lock) rather than a `pg_locks` introspection. Arbitrary string lock names are hashed (FNV-1a 64-bit) into the two-int advisory key. Every
  release runs on a fresh, bounded context so a canceled request context cannot strand the lock, and if `pg_advisory_unlock` cannot be issued the physical session is ended instead (the driver connection is marked bad so `database/sql` closes rather than pools it), which releases the lock server-side and guarantees a still-held lock is never returned to the pool for reuse. `WithLockReleaseTimeout` tunes the release timeout (default 5s).

### Fixed

- `go.mod` — the framework pin is raised from `v3.0.0` to `v3.7.0`, the lowest version providing the newly imported `lock/contract` package, so the module resolves outside the repository workspace; the module-local `go.sum` is now complete for standalone builds.
- `provider.go` — the connection-retry backoff no longer collapses to zero at very high attempt counts: `computeBackoffDelay` now grows the delay in float space and returns `MaxDelay` as soon as it is reached, instead of converting `float64(initialDelay) * multiplier` to `time.Duration` first — for an aggressive `RetryConfig` (e.g. `MaxAttempts >= 37` with the default 2× multiplier) the product overflowed `int64` to a negative duration that slipped past the `> MaxDelay` cap, so `time.Sleep` returned immediately and late attempts re-dialled with no backoff.
- `provider.go` — `isTransientError` now also treats bare `EOF`/`unexpected EOF`, `use of closed network connection`, `connection closed`, and `connection reset` as transient, so a graceful peer close during the handshake phase of a cold-starting database is retried instead of hard-failing on the first attempt.

## [v3.1.1] - 2026-06-16 - Honor Zero ConnectTimeout on the Connection Ping

### Added

- `v3/README.md` — added a v3 module README documenting the option-based `Provider`, the secure-by-default TLS controls (`WithInsecure`/`WithTlsConfig`), the typed pool/timeout/retry configs, and the post-build hook; also documented that the package ships no self-registering application module (provider-only — PostgreSQL exposes no application-level service, unlike the MySQL advisory-lock module), so it is registered through the core registry.

### Changed

- `v3/provider.go` — the retry/backoff fallbacks in `openWithRetry`/`computeBackoffDelay` now read from `DefaultRetryConfig()` instead of repeating the `3` / `500ms` / `5s` / `2.0` literals inline, so the documented defaults and the zero-value fallbacks cannot drift apart. Behaviour is unchanged.

### Fixed

- `v3/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout (`WithTimeoutConfig(NewTimeoutConfig(0))`, which the framework treats elsewhere — and in the same function's post-build-hook block — as "no deadline / wait indefinitely") produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a perfectly healthy database, surfacing as `"database connection failed"`. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, mirroring the bunorm `mysql/v3` provider.
- `v3/provider.go` — `openWithRetry` no longer panics when `Open` is called with a `nil` logger and a `RetryConfig`. The retry path called `logger.Info`/`Warning`/`Error` directly, so a transient connection error dereferenced the nil logger; the logger is now normalized through `logging.EnsureLogger`, matching the framework's nil-logger contract that the non-retry path (and the example wiring) already rely on.

## [v3.1.0] - 2026-04-23 - Default TLS Handshake (MEL-161)

### Added

- `provider_option.go` — `WithInsecure(insecure bool) ProviderOption` lets callers toggle the `pgdriver.WithInsecure(...)` flag (default `false`) (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option.go` — `WithTlsConfig(config *tls.Config) ProviderOption` lets callers pass a `*crypto/tls.Config` that is forwarded to `pgdriver.WithTLSConfig(...)`. When a non-nil `tls.Config` is supplied, it takes precedence over `WithInsecure(...)` (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option_test.go` — coverage for default (`insecure=false`, `tlsConfig=nil`), `WithInsecure(true)` override, and `WithTlsConfig(...)` field storage; mirrored in `v2/` and `v3/`

### Changed

- `provider.go` — `Open(...)` now builds the `pgdriver` connector from `instance.insecure` / `instance.tlsConfig` instead of hardcoding `pgdriver.WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

### Fixed

- `provider.go` — default TLS handshake is now enabled. The legacy hardcoded `pgdriver.WithInsecure(true)` silently disabled TLS on every Postgres connection; `insecure` now defaults to `false`, so new `NewProvider(...)` callers negotiate TLS out of the box. Operators who still rely on plain-TCP can opt in with `WithInsecure(true)`. This is a **behavioural change**: deployments without a TLS-capable Postgres endpoint must either expose TLS on the server or explicitly pass `WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.3...HEAD

[v3.2.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.2...integrations/bunorm/pgsql/v3.2.3

[v3.2.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.1...integrations/bunorm/pgsql/v3.2.2

[v3.2.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.0...integrations/bunorm/pgsql/v3.2.1

[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.1...integrations/bunorm/pgsql/v3.2.0

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.0...integrations/bunorm/pgsql/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.1...integrations/bunorm/pgsql/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.0...integrations/bunorm/pgsql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v3.0.0
