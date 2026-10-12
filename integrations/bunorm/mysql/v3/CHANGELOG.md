# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Unlimited` lifts the bound of a `PoolConfig` or `TimeoutConfig` field — no deadline, no cap on open or idle connections, no recycling; every negative value reads the same, so `-1s` from configuration lifts it too, while a zero field takes the default: "keep no idle connection" is `SetMaxIdleConns(0)` on the `*bun.DB` the provider returns.
- `ConnectionConfig` and `NewConnectionConfig` hold the connection details the provider dials; `SafeContext` answers them without the password, for a log line or an error context.
- opening a connection routes bun's own diagnostic channel into the application's journal, once per process, through `bunorm.RouteDiagnostics`, so bun's reports of a declaration mistake arrive as warning records; the dialect's `can't discover MySQL version` stays on standard error, since routing it would take `log.SetOutput` for the whole process (see the readme); see the `bunorm` changelog for the door itself.
- `WithInsecure` and `WithTlsConfig`, as on pgsql: `WithInsecure(true)` leaves the connection plaintext, the deliberate opt-out from the verified default; `WithTlsConfig` hands the connector an explicit configuration, taking precedence over both the default and `WithInsecure`.
- `Provider.OpenContext` implements `bunorm.ContextOpener` and `Provider.OpenForMigrationContext` implements `bunorm.MigrationContextOpener`: the open and its retry sleeps run under the caller's context, so an already-cancelled open is refused before the attempt and a cancellation mid-retry is honoured instead of sleeping out the budget, reported with the last attempt's failure as context; `OpenForMigration` is the same call under `context.Background()`.
- the provider implements `bunorm.MigrationProvider`: `OpenForMigration` opens the database with the read and write deadlines lifted and the connect timeout kept, over a two-connection pool with no mid-run recycling, so a migration whose DDL runs past the 30s request deadlines finishes instead of dying with `invalid connection`.

### Changed

- **Behavioural change**: the provider negotiates a verified TLS handshake by default, as pgsql does — the system roots, the configured host as the name to verify, `MinVersion` TLS 1.2 in a `tls.Config` — refusing the driver's `AllowFallbackToPlaintext` and `skip-verify` spellings; it used to connect in plaintext by default, TLS reachable only by mutating `*mysql.Config` in a `PostBuildHook`. A mysql that speaks no TLS now fails the dial; arm `WithInsecure(true)` for it — see the framework's `UPGRADE.md`.
- **Behavioural change**: the `bun` requirement moves to `v1.2.17`, the dialect and driver packages in lockstep (the dialects panic at init on a version mismatch): v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over an unchanged schema.
- credential redaction is the application's call on this major: the provider is handed connection VALUES, not parameter names, so it names no credential; the party that resolved the values marks them, through `RegisterSecretParameter` for a parameter the application declares or `MarkParameterSecret` for one melody registered from `.env`, and the mark propagates to every parameter whose template reads the secret, the assembled dsn included, where v1's `Provider.Open` used `MarkSecret`; the retry loop's terminal record is the log of the failure, written in full and returned with the already-logged mark, so the exit handler no longer writes the outage twice.
- documentation: the readme documents `IsDuplicateKey`, published since the first releases with no line anywhere — whether an error, or one it wraps, is the driver's duplicate key refusal (1062) — and `ConnectionConfig`, whose `SafeContext` carries every failed open without the password.

### Fixed

- **Behavioural change**: a refusal the server gave by name is never retried and never falls to the primary — see the framework's `UPGRADE.md`. The transient classifier reads the caller's context first, then the server's error number, then the message, where an operand quoted in the message (a database named `timeout`, a user `eof`) made a permanent refusal transient, retried and filed under `ErrDatabaseUnreachable`, the class `ReadWriteSplitter.Reader` serves the primary for: 1040, 1053, 1203, 1226 and 1159 (the server's own timeout, answered at `connect_timeout`), and ProxySQL's 9001 and 9002, stay transient; every other number, 1045 and 1049 among them, is a refusal (the number rather than the SQLSTATE, the server filing resource refusals under `42000` and `HY000`); a caller whose context was already done gets a clean stop.
- **Behavioural change**: an empty host is refused by name before the driver configuration is built, where the driver's `:port` connected the application to whatever listened on that port of its own machine, with the configured credentials; the database and user are left to the server and an empty password stays legitimate — see the framework's `UPGRADE.md`.
- **Behavioural change**: the transient classifier matches its markers as words, a boundary being any character but a letter, a digit or an underscore: `eof` inside a table named `geofences`, `timeout` in a `session_timeout` column retried a schema error for the whole budget; the `io.EOF` and `net.Error` checks are untouched. Such a failure now fails fast — see the framework's `UPGRADE.md`.
- **Behavioural change**: every zero field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default and a negative one lifts the bound, spelled by `Unlimited` (in `provider.go`): a zero from an unset environment key made `database/sql` an unlimited, never-recycled pool and the driver a read or write with no deadline, and a negative connect timeout failed every dial instantly. A configuration relying on zero meaning "unlimited" asks for it with `Unlimited`; the migration connection keeps its lifted deadlines and disabled recycling.
- the IPv6 host: a bare literal dials the host it names, bracketed unless it already is, where the address joined as `host:port` made `::1` into `::1:3306` and go-sql-driver re-joined it into `[::1:3306]:3306`, dialled through the whole retry budget; the default verifying TLS config verifies the host without brackets or zone, so `[::1]` and `fe80::1%eth0` can be verified.
- the first connection is established by a ping under the connect timeout and the caller's context before bun is handed the pool, where bun's dialect query dialled it with no context, so a peer that never sent the server greeting blocked `OpenForMigration` for ever and held the request pool's open for its whole read deadline.
- an open failure the transient classifier admitted — the retry budget could not get past it, or the retry-less open met it — is filed under `bunorm.ErrDatabaseUnreachable`, keeping its message, context and the driver failure under the class, the one class a `ReadWriteSplitter` absorbs by reading the primary, and a refusal given by name is not, so a misconfigured replica is refused rather than silently served from the primary; the retry backoff has a floor of one millisecond, applied to the initial delay and the ceiling, and is a single exponentiation capped before conversion, an attempt of zero reading as the first; a connection abort is recognised in `provider.go` by both its platform spellings, `software caused connection abort` and `established connection was aborted`.
- the named lock: every failure names the folded form the server was asked for beside the caller's name, a name past 64 characters being folded to a hash-suffixed form; a refresh or re-acquire whose `IS_USED_LOCK() = CONNECTION_ID()` probe could not be answered no longer issues `RELEASE_LOCK` and drops the session, which handed away a lock still held and let `RunExclusive` put a second holder in the exclusive section, the re-acquire answering `(false, nil)`: only a session that is gone loses the lock, as on pgsql.
- the diagnostics of `provider.go`: the retry warning, a cancellation during the backoff — whose cause stays the cancellation, the outage riding as structured context, marked as already logged — and the connection-failure record carry the host and port dialled, the pool sizing, the deadlines and the cause chain through `exception.LogContext`, where the first two had `openErr.Error()` flattened; the dialled endpoint is carried beside the configured one, a post-build hook being free to rewrite it; the caller's own cancellation, `context.Canceled` only, is a clean stop recorded at warning and not retried, where a SIGTERM mid-deploy paged with "non-transient error"; a nil logger, typed nil included, is the emergency logger in `OpenContext` and receives bun's diagnostics routing, where a discard sink silenced the outage and bun's warnings for every later open.
- the negotiated TLS posture is proven where the driver receives it: the post-build hook is handed the connector's configuration and three tests read the verifying default, the plaintext path behind `WithInsecure` and an explicit `WithTlsConfig` through it.
- documentation: the README says how the host is joined with the port — an IPv6 literal bare or bracketed, a bare one bracketed, a bracketed one kept, a scoped one keeping its zone; the unreleased entries spell the behavioural-change marker in full and name `ConnectionConfig`.

## [v3.1.4] - 2026-07-24 - Server Shutdown Transient Marker

### Fixed

- the connection retry treats `Server shutdown in progress` as transient, so a restarting server is retried instead of failing the open on the first attempt

## [v3.1.3] - 2026-07-17 - Connection-Retry Backoff Clamps

### Fixed

- `provider.go` — the connection retry backoff rejects degenerate values: a non-positive `InitialDelay` or `MaxDelay` and a `BackoffMultiplier` that is not at least 1 — a NaN included, which fails every comparison and so slipped through a below-1 check, poisoned the float-space growth and converted to a negative duration — fall back to the defaults instead of collapsing every retry into an immediate re-dial (a negative delay made the sleep return immediately; a sub-1 multiplier decayed the delay toward zero). A multiplier of exactly 1 remains a valid constant backoff.

## [v3.1.2] - 2026-07-11 - Advisory Lock Name Hashing

### Fixed

- `lock_test.go` — the genuine-verify-error test no longer races MySQL's asynchronous `KILL`. The server flags the killed thread and releases its locks only once that thread notices, so the immediate re-acquire read `GET_LOCK` as still-held (`false`, no error) and the suite failed intermittently under load. The attempt now retries until the dead session lets go.
- `lock.go` — the liveness verify on an idempotent re-`Acquire` runs on a fresh, bounded context, as `Refresh` already did. It ran on the caller's request context, so a transient cancellation was mistaken for a lost lock and actively `RELEASE_LOCK`ed a lock this process still held.
- `lock.go` — the MySQL locker derives a bounded, `GET_LOCK`-compatible lock name, so a name longer than MySQL's 64-character user-level-lock limit no longer makes `Acquire` fail permanently — which had kept exclusive commands and the jobs they wrap from ever running. A name within the limit is unchanged.

## [v3.1.1] - 2026-07-06 - Standalone Module Resolution and Connection-Retry Fixes

### Fixed

- `lock.go` — the advisory-lock (`GET_LOCK`) locker now marks the pinned driver connection bad (`driver.ErrBadConn`) when `RELEASE_LOCK` cannot be issued, so `database/sql` ends the physical session — releasing the lock server-side — instead of returning a still-locked connection to the pool for reuse; and `Refresh` now probes lock ownership on a fresh, bounded context rather than the request context, so a canceled or expired request context is never mistaken for a lost lock and does not actively release a still-held one. Both bring the MySQL locker to parity with the PostgreSQL advisory-lock locker, which already handled these cases.
- `go.mod` — the module pinned `melody/v3 v3.0.0` while importing the `lock`/`lock/contract` packages, which only exist from `v3.7.0`: outside the repository workspace (`GOWORK=off`, or any consumer cloning just this module) the module did not resolve. The pin is raised to `v3.7.0` — the lowest framework version that provides every imported package — and the module-local `go.sum` is now complete for standalone builds.
- `provider.go` — the connection-retry backoff no longer collapses to zero at very high attempt counts: `computeBackoffDelay` now grows the delay in float space and returns `MaxDelay` as soon as it is reached, instead of converting `float64(initialDelay) * multiplier` to `time.Duration` first — for an aggressive `RetryConfig` (e.g. `MaxAttempts >= 37` with the default 2× multiplier) the product overflowed `int64` to a negative duration that slipped past the `> MaxDelay` cap, so `time.Sleep` returned immediately and late attempts re-dialled with no backoff.
- `provider.go` — `isTransientError` now also treats bare `EOF`/`unexpected EOF`, `use of closed network connection`, `connection closed`, and `connection reset` as transient, so a graceful peer close during the RESP/handshake phase of a cold-starting server is retried instead of hard-failing on the first attempt.

## [v3.1.0] - 2026-06-16 - MySQL Advisory Lock (GET_LOCK)

### Added

- `v3/service_resolver.go` — `RegisterLockerService(registrar, database)` registers the MySQL `GET_LOCK` locker under the core `lock.ServiceLocker`, so userland wires it into the container in one call.
- `v3/module.go` — `mysql.NewModule(ModuleConfig{Database, AsLocker})` self-registering application module that registers the MySQL advisory-lock locker service (opt-in via `AsLocker`, skipped when the database is nil), replacing a hand-written `RegisterLockerService` call. v3 only.
- `v3/lock.go` — MySQL `GET_LOCK`-backed implementation of the core `lock/contract.Locker`/`Lock`. `NewLocker(database)` creates named locks; `Acquire` is non-blocking (`GET_LOCK(?, 0)`, consistent with the try-acquire semantics of the in-memory and Redis backends) and takes a session-scoped lock on a dedicated `*sql.Conn` that is pinned for the lifetime of the held lock and released (`RELEASE_LOCK`) on the same connection. MySQL advisory locks are connection-lifetime: they do not auto-expire, so the `CreateLock(name, ttl)` `ttl` is accepted only for interface compatibility and is documented as not honored as an expiry; `Refresh` therefore has nothing to extend but verifies the lock is still held on its connection (`IS_USED_LOCK(?) = CONNECTION_ID()`) and returns a "lock is no longer held" error if it has been lost, matching the lost-lock signal of the in-memory and Redis backends.
- `v3/lock.go` — `WithLockReleaseTimeout(time.Duration)` option on `NewLocker(database, ...options)` exposes the fresh-context timeout used to release a `GET_LOCK` (previously a hardcoded `5s` `lockReleaseTimeout` constant, applied across `Release`, the orphaned-lock cleanup, and the stale-connection release); zero keeps the 5s default.

### Changed

- `v3/provider.go` — the retry/backoff fallbacks in `openWithRetry`/`computeBackoffDelay` now read from `DefaultRetryConfig()` instead of repeating the `3` / `500ms` / `5s` / `2.0` literals inline, so the documented defaults and the zero-value fallbacks cannot drift apart. Behaviour is unchanged.

### Fixed

- `v3/lock.go` — when `Refresh` detects the lock was lost (the owning session was killed or the lock forcibly released) or its probe query errors, it now closes and clears the pinned `*sql.Conn` before returning the error. Previously the connection was left set, so the next `Acquire` took the "already held" fast path (`nil != instance.connection`) and falsely reported the lock as held without re-issuing `GET_LOCK`, breaking mutual exclusion after a lock-loss event.
- `v3/lock.go` — a reentrant `Acquire` on a still-set connection now re-verifies ownership (`IS_USED_LOCK(?) = CONNECTION_ID()`) before taking the fast path, and transparently re-acquires on a fresh connection if the pinned one was dropped. Previously a reentrant `Acquire` made *without* an intervening `Refresh` returned `(true, nil)` purely because `instance.connection` was non-nil, so if that connection had died (and MySQL had already auto-released the lock) the original holder and a competitor that grabbed the freed lock could both believe they held it at once.
- `v3/lock.go` — the initial-acquisition path of `Acquire` now best-effort `RELEASE_LOCK`s before closing the dedicated `*sql.Conn` when the `GET_LOCK(?, 0)` probe errors after the lock may already have been taken server-side — for example when the runtime context is cancelled in the window between the server granting the lock and the client reading the result row. Previously this path bare-`Close`d the connection, and because closing a `*sql.Conn` returns the session to the pool **without** running `RELEASE_LOCK`, a lock the server had already granted could be orphaned on the pooled session; the cleanup mirrors the reentrant-verify error paths below and runs on a short bounded background context so a cancelled request context cannot prevent it.
- `v3/lock.go` — the reentrant-verify error paths of `Acquire` and `Refresh` now best-effort `RELEASE_LOCK` before closing the pinned `*sql.Conn`, so a still-held named lock is no longer orphaned in the pool. Closing a `*sql.Conn` returns the underlying MySQL session to the pool **without** releasing its session-scoped `GET_LOCK` (the driver's session reset does not run `RELEASE_ALL_LOCKS`), so when the ownership probe failed for a reason other than a dead session — for example the runtime context was already cancelled or the probe hit a transient/timed-out query while the session was alive and still owned the lock — the lock stayed held by the pooled session with nothing referencing it, blocking every subsequent acquirer of that name until the connection was recycled (up to `ConnMaxLifetime`). The release runs on a short bounded background context so a cancelled request context cannot prevent the cleanup, mirroring the `RELEASE_LOCK`-before-`Close` that the normal `Release` path
  already performs.
- `v3/provider.go` — `open` now treats a zero `ConnectTimeout` as "no deadline" on the connectivity ping, matching the guard already applied to the post-build hook a few lines above. The ping wrapped the context with `context.WithTimeout(ctx, 0)` unconditionally, and a zero duration yields an already-expired deadline, so a provider built with `NewTimeoutConfig(0, …)` failed every `Open` with `database connection failed` against a fully reachable database. The two code paths now interpret a zero `ConnectTimeout` identically.
- `v3/provider.go` — `openWithRetry` no longer panics when `Open` is called with a `nil` logger and a `RetryConfig`. The retry path called `logger.Info`/`Warning`/`Error` directly, so a transient connection error dereferenced the nil logger; the logger is now normalized through `logging.EnsureLogger`, matching the framework's nil-logger contract that the non-retry path (and the example wiring) already rely on.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.4...HEAD

[v3.1.4]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.3...integrations/bunorm/mysql/v3.1.4

[v3.1.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.2...integrations/bunorm/mysql/v3.1.3

[v3.1.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.1...integrations/bunorm/mysql/v3.1.2

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.1.0...integrations/bunorm/mysql/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.2...integrations/bunorm/mysql/v3.1.0

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.1...integrations/bunorm/mysql/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v3.0.0...integrations/bunorm/mysql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v3.0.0
