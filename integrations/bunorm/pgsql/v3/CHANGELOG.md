# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.3.0] - 2026-10-19 - Named Driver Deadlines, Unlimited Bounds, Context-Aware Opens, Routed Diagnostics and Typed Duplicate-Key Reading

### Added

- `Unlimited` lifts the bound of a `PoolConfig` or `TimeoutConfig` field — no deadline, no cap on open or idle connections, no recycling; every negative value reads the same, so `-1s` from configuration lifts it too, while a zero field takes the default: "keep no idle connection" is `SetMaxIdleConns(0)` on the `*bun.DB` the provider returns.
- `ConnectionConfig` and `NewConnectionConfig` hold the connection details the provider dials; `SafeContext` answers them without the password, for a log line or an error context.
- `RegisterLockerService(registrar, database)` and `NewModule(ModuleConfig{Database, AsLocker})`, as on mysql: the helper publishes the advisory-lock `Locker` (shipped since v3.2.0) under the framework's `lock.ServiceLocker` name, so an application resolving "the locker" receives whichever backend it wired; the module is the same registration behind one `RegisterModule` call, opt-in through `AsLocker`, skipped without a database.
- opening a connection routes bun's own diagnostic channel into the application's journal, once per process, through `bunorm.RouteDiagnostics`, so bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule, a query with arguments and no placeholders — arrive as warning records; see the `bunorm` changelog for the door. The pgsql dialect writes nothing through the standard library's logger, so this covers everything bun reports here.
- `Provider.OpenContext` implements `bunorm.ContextOpener`, `Provider.OpenForMigrationContext` `bunorm.MigrationContextOpener` and `Provider.OpenForMigration` `bunorm.MigrationProvider`, as on mysql: the open and its retry sleeps run under the caller's context, an already-cancelled open refused before the attempt and a cancellation arriving mid-attempt honoured at the next cancellable step instead of sleeping out the budget, `OpenForMigration` being the same call under `context.Background()`; the migration open lifts the driver deadlines over a two-connection pool with no mid-run recycling, where the migrate commands fell back to the request pool and pgdriver's unconfigured 10-second read deadline cut a longer DDL statement, an 11-second statement having died at 10.004s with an i/o timeout.

### Changed

- `go.mod` — requires `github.com/precision-soft/melody/v3` at `v3.14.0`, raised from `v3.7.0`, and `github.com/precision-soft/melody/integrations/bunorm/v3` at `v3.6.0`, raised from `v3.0.0`, the bunorm release that requires that framework version; a module that requires this release is pulled up to those versions with it
- **Behavioural change**: `TimeoutConfig` names every deadline the driver applies — `ReadTimeout` and `WriteTimeout` join `ConnectTimeout` — and the connector receives all three, the dial included, where the connect timeout never reached the dial (pgdriver's 5-second default) and the read/write deadlines were the driver's invisible 10s/5s; the effective deadlines move to the documented 30s/30s, as on mysql. `NewTimeoutConfig(connect)` keeps its arity and `NewTimeoutConfigWithDeadlines` sets all three.
- **Behavioural change**: the `bun` requirement moves to `v1.2.17`, the dialect and driver packages in lockstep (the dialects panic at init on a version mismatch): v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over an unchanged schema.
- credential redaction is the application's call on this major: the provider is handed connection VALUES, not parameter names, so it names no credential; the party that resolved the values marks them, through `RegisterSecretParameter` for a parameter the application declares or `MarkParameterSecret` for one melody registered from `.env`, and the mark propagates to every parameter whose template reads the secret, the assembled dsn included, where v1's `Provider.Open` used `MarkSecret`; the retry loop's terminal record is the log of the failure, written in full and returned with the already-logged mark, so the exit handler no longer writes the outage twice.

### Fixed

- lock: `Refresh`'s refusal of a lost lock carries the liveness probe's failure as its cause, as the mysql twin does.
- **Behavioural change**: a refusal the server gave by name is never retried and never falls to the primary. The transient classifier reads the caller's context first, then the SQLSTATE class, then the message, where an operand quoted in the message (a database named `timeout`, a user `eof`) made a permanent refusal transient, retried and filed under `ErrDatabaseUnreachable`, the class `ReadWriteSplitter.Reader` serves the primary for: `08`, `53` and `57` stay transient, so an `08006` or a `57P03` with no marker in its message is no longer terminal, and every other class is a refusal; a caller whose context was already done gets a clean stop.
- **Behavioural change**: an empty host is refused by name, where the driver's `:port` connected the application to whatever listened on that port of its own machine, with the configured credentials and the insecure posture; an empty database name or user is refused by name too, where `pgdriver.WithDatabase` and `pgdriver.WithUser` panicked out of the open; an empty password stays legitimate.
- **Behavioural change**: the transient classifier matches its markers as words, a boundary being any character but a letter, a digit or an underscore: `eof` inside a table named `geofences`, `timeout` in a `session_timeout` column retried a schema error for the whole budget; the `io.EOF` and `net.Error` checks are untouched. Such a failure now fails fast.
- **Behavioural change**: every zero field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default and a negative one lifts the bound, spelled by `Unlimited` (in `provider.go`): a zero from an unset environment key made `database/sql` an unlimited, never-recycled pool, and a non-positive connect timeout left the startup ping with no deadline. A configuration relying on zero meaning "unlimited" asks for it with `Unlimited`.
- **Behavioural change**: `IsDuplicateKey` answers on the typed SQLSTATE `23505`, matched through `errors.As`, where a substring probe of the message matched any text with the digits and missed a duplicate wrapped under a message hiding its cause; an error carrying no PostgreSQL protocol error — a hand-written "duplicate key", one stringified across a process boundary — no longer matches.
- `IsDuplicateKey` reads the SQLSTATE through `SQLState()` as well as pgdriver's `Field('C')`, so a collision through pgx's `pgconn.PgError` or lib/pq's `Error`, which implement no `Field`, answers a conflict rather than a server failure.
- the IPv6 host: a bare literal dials the host it names, bracketed unless it already is, where the address joined as `host:port` made `::1` into `::1:5432`, refused by pgdriver as an address with too many colons; the default verifying TLS config verifies the host without brackets or zone, so `[::1]` and `fe80::1%eth0` can be verified.
- an open failure the transient classifier admitted — the retry budget could not get past it, or the retry-less open met it — is filed under `bunorm.ErrDatabaseUnreachable`, keeping its message, context and the driver failure under the class, the one class a `ReadWriteSplitter` absorbs by reading the primary, and a refusal given by name is not, so a misconfigured replica is refused rather than silently served from the primary; the retry backoff has a floor of one millisecond, applied to the initial delay and the ceiling, and is a single exponentiation capped before conversion, an attempt of zero reading as the first; a connection abort is recognised in `provider.go` by both its platform spellings, `software caused connection abort` and `established connection was aborted`, replacing the deprecated `net.Error.Temporary()`.
- every failure of the advisory lock names the two halves of the key the server was asked for beside the caller's name, to be matched against `pg_locks`.
- the diagnostics of `provider.go`: the retry warning and a cancellation during the backoff — whose cause stays the cancellation, the outage riding as structured context, marked as already logged — carry the host and port dialled, the pool sizing, the deadlines and the cause chain through `exception.LogContext`, where they had `openErr.Error()` flattened; the dialled endpoint is carried beside the configured one, a post-build hook being free to rewrite it; the caller's own cancellation, `context.Canceled` only, is a clean stop recorded at warning and not retried, where a SIGTERM mid-deploy paged with "non-transient error"; a nil logger, typed nil included, is the emergency logger for the open's own records in `OpenContext`, where a discard sink silenced the outage; `resolvedPoolConfig` keeps the lifted `ConnectionMaxLifetime`/`ConnectionMaxIdleTime` zeros of the migration connection, as `resolvedTimeoutConfig` does, where they were re-armed and recycled five minutes into a run under
  `OpenForMigration`.
- the negotiated TLS posture is proven where the driver receives it: the post-build hook is handed the connector's configuration and three tests read the verifying default, the plaintext path behind `WithInsecure` and an explicit `WithTlsConfig` through it.
- documentation: the README says how the host is joined with the port — an IPv6 literal bare or bracketed, a bare one bracketed, a bracketed one kept, a scoped one keeping its zone — and that the package ships a registration helper and an application module for its advisory-lock `Locker`; the GoDoc of `Locker` says it holds the database handle and the release timeout, fixed at construction, and that an advisory lock lives on the connection that took it, so one locker serves concurrent callers without a lock of its own; the unreleased entries spell the behavioural-change marker in full and name `ConnectionConfig`.
- an open handed no logger, like one refused under an already-cancelled context, routes bun's diagnostic channel nowhere, as the readme says, instead of moving it onto the emergency logger: the provider substituted the emergency logger for its own records before the routing read the logger, so a migration command or a tool opening without a journal took the channel away from the application's journal and onto standard error; the routing now reads the logger as handed, and the emergency logger serves the open's own records alone.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.3.0...HEAD

[v3.3.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.3...integrations/bunorm/pgsql/v3.3.0

[v3.2.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.2...integrations/bunorm/pgsql/v3.2.3

[v3.2.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.1...integrations/bunorm/pgsql/v3.2.2

[v3.2.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.2.0...integrations/bunorm/pgsql/v3.2.1

[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.1...integrations/bunorm/pgsql/v3.2.0

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.0...integrations/bunorm/pgsql/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.1...integrations/bunorm/pgsql/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.0...integrations/bunorm/pgsql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v3.0.0
