# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- the `bun` requirement moves to `v1.2.17`, with the dialect and driver packages in lockstep: the dialects verify at init that their version equals bun's and panic otherwise. v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over a schema that never changed; the whole family moves together so no binary can assemble a mismatched pair

### Added

- the provider implements `bunorm.MigrationProvider`: `OpenForMigration` opens the same database with the read and write deadlines lifted and the connect timeout kept — a down database must still fail fast — over a pool of the two connections a sequential migration run needs, with no mid-run connection recycling, because a lifetime rotation under a running statement is the same mid-statement cut by another name. This is what lets a migration whose DDL legitimately runs past the request-sized 30s deadlines finish instead of dying with `invalid connection` at step N of M

### Fixed

- `provider.go` — transient-error detection recognises a connection abort through explicit markers for both spellings its platforms give it (`software caused connection abort` and `established connection was aborted`), aligning with the pgsql provider

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
