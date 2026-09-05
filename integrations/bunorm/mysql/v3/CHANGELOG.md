# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- opening a connection routes bun's own diagnostic channel into the application's journal, once per process, through `bunorm.RouteDiagnostics`: bun's reports of a declaration mistake — an unknown struct tag option, a query carrying arguments and no placeholders — arrive as warning records instead of unstructured lines on standard error. See the `bunorm` changelog for the door itself, and this package's readme for the one line that deliberately stays on standard error: the dialect writes `can't discover MySQL version` through the standard library's default logger, not bun's, so routing it would mean taking `log.SetOutput` for the whole process

- `WithInsecure` and `WithTlsConfig`, the two provider options its pgsql sibling already carried: `WithInsecure(true)` leaves the connection plaintext, the deliberate opt-out from the verified default; `WithTlsConfig` hands the connector an explicit configuration — a pinned server certificate, a client certificate — taking precedence over both the default and `WithInsecure`
- `Provider.OpenForMigrationContext` — the provider implements `bunorm.MigrationContextOpener`: the migration open runs under the caller's context, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. `OpenForMigration` is the same call under `context.Background()`, so no existing call site changes

- `Provider.OpenContext` — the provider implements `bunorm.ContextOpener`: the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget, and the cancellation is reported with the last attempt's own failure as context

- the provider implements `bunorm.MigrationProvider`: `OpenForMigration` opens the same database with the read and write deadlines lifted and the connect timeout kept — a down database must still fail fast — over a pool of the two connections a sequential migration run needs, with no mid-run connection recycling, because a lifetime rotation under a running statement is the same mid-statement cut by another name. This is what lets a migration whose DDL legitimately runs past the request-sized 30s deadlines finish instead of dying with `invalid connection` at step N of M

### Changed

- the provider negotiates a verified TLS handshake by default, the security posture its pgsql sibling already carried. The provider set no TLS on the connector, so it connected in plaintext and offered no option to turn TLS on — the only path was mutating `*mysql.Config` inside a `PostBuildHook`. It now builds a verifying `tls.Config` (the system roots, the configured host as the name to verify against, `MinVersion` TLS 1.2) by default, refusing the driver's `AllowFallbackToPlaintext` and `skip-verify` spellings that would downgrade silently or negotiate TLS without checking the certificate. **Behavioural change**: a mysql that speaks no TLS now fails the dial where it previously connected in plaintext; a caller reached over a trusted network or against such a server arms `WithInsecure(true)` explicitly, the same opt-out spelled the same way as pgsql — see the framework's `UPGRADE.md`
- credential redaction is the application's call on this major, not the provider's and not the registry's: v1's `Provider.Open` marks its configured password parameter through the configuration's own `MarkSecret`, but this major hands the provider the connection VALUES rather than the parameter names it would read them under, so the provider knows no configuration key and names no credential. The door is the framework's own parameter registrar — `RegisterSecretParameter` for a parameter the application declares, `MarkParameterSecret` for one melody registered from the `.env` artifacts — called by the party that resolved the values. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it
- the terminal failure record of the retry loop is the log of that failure: it is written in full through the exception context and the returned error carries the already-logged mark, so the exit handler no longer writes the same outage a second time

- the `bun` requirement moves to `v1.2.17`, with the dialect and driver packages in lockstep: the dialects verify at init that their version equals bun's and panic otherwise. v1.2.16 swallowed the failure of a migration read from a `.sql` file, which `integrations/bunorm/migrate` answered with `[success]` and exit 0 over a schema that never changed; the whole family moves together so no binary can assemble a mismatched pair

### Fixed

- every failure of the named lock names the folded form the server was asked for beside the caller's name. A name past the server's 64-character limit is folded to a hash-suffixed form, and a diagnostic that showed only the caller's spelling sent the operator to look for a lock the server had never heard of.

- a refresh or a re-acquire whose probe could not be ANSWERED no longer hands away the lock it was asking about. The probe reads `IS_USED_LOCK() = CONNECTION_ID()`, and on any error at all — a server stall past the probe budget, a killed query, a blip on the wire — both doors issued `RELEASE_LOCK` on the pinned session and dropped it. But MySQL holds a named lock for exactly as long as the session that took it, so none of those causes had lost the lock: the process released one it was still holding, and `RunExclusive`, which reads a failed refresh as "another instance may hold it now", stopped the callback — so the two together put a SECOND holder inside an exclusive section this one had never left, which is the one thing an exclusive section exists to prevent. On the re-acquire path the same branch then answered `(false, nil)`, telling the caller it had never held a lock it was holding a moment earlier. Both doors now ask the question that actually decides — is the session still up — and only a session that is genuinely gone loses the lock, its connection ended without an unlock because a dead session released its locks server-side already. This is the policy the code's own comment claimed and the policy its pgsql sibling implements, which that comment named as the model. The refusal that a cancelled caller context must not look like a lost lock is unchanged; it was one member of a class that has others

- the retry backoff has a FLOOR of one millisecond and is computed in closed form. The configuration guards refused a non-positive delay, which left one NANOSECOND as the smallest thing an operator could ask for — a wait shorter than the dial it separates, which is the re-dial storm those guards exist to prevent arriving through the door they left open. The floor is applied to the initial delay and to the ceiling, so no branch can answer under it. Separately, the growth was walked once per attempt already made — O(attempt) per call and therefore O(attempt²) over a run — and it left the walk early only once the delay had passed the ceiling, which a multiplier of exactly 1, a valid constant backoff, never does; a large attempt budget paid that square in full for a delay that never moved. The growth is now a single exponentiation, capped before the conversion exactly as the walk was, so a large attempt count still cannot overflow into a negative duration. An attempt of zero reads as the first attempt, which is what the walk answered it by never running
- the transient classifier matches its markers as words rather than as bare substrings. The short spellings fired inside ordinary identifiers — `eof` sits inside a table named `geofences`, `timeout` inside a `session_timeout` column — so a permanent failure was retried for the whole budget and then reported as "failed after max retry attempts" rather than as non-transient, costing the delay and telling the operator the wrong thing about what is really a schema error. A boundary is any character that is not a letter, a digit or an underscore, so the spellings carrying spaces and slashes match exactly as before, and the `io.EOF` and `net.Error` checks above the scan are untouched. **Behavioural change**: a failure whose message merely contains a marker inside a word now fails fast instead of exhausting the retry budget — see the framework's `UPGRADE.md`
- a retry cancelled during its backoff hands on the failure it was retrying as structured context rather than as a flattened string. The cause stays the cancellation, because the classification upstream reads it, but the outage arrived as `openErr.Error()` in a single field — the exact flattening the comment eleven lines above condemns for the retry warning, which lifts the failure's own context and its cause chain. The record now carries the host and port dialled, the pool sizing and the deadlines that governed the attempt, so one failure has one record shape whether or not the caller happened to cancel
- the diagnostic of a failed connection names the endpoint the dial actually reached. The connection context is built from the configuration parameters before the post-build hook runs, and the hook may rewrite the address — it is handed the very field for it — so a ping failure named the configured host and port while the attempt had gone somewhere else. The dialled address is carried beside the configured connection rather than folded into it, so a record where the two differ says so
- the negotiated TLS posture is proven where the driver receives it, not only in the helper that computes it. The helper had its own test, but nothing observed that its answer reached the connector, so a deleted assignment would have left every default connection in plaintext with the suite green. The post-build hook is handed the configuration the connector is built from, and three tests read the posture through it: the verifying default with the configured host as the name to verify against, the one plaintext path behind `WithInsecure`, and an explicit `WithTlsConfig` reaching the driver exactly as it was given
- `provider.go` — the retry warning carries the diagnostic shape the terminal records carry: the host and port dialed, the pool sizing, the deadlines that governed the attempt and the cause chain, lifted through `exception.LogContext` the way the three records above it already are. The first two records an operator sees when a database is down had the failure flattened to `openErr.Error()` — a message and nothing to act on — and only the third, terminal one named the database

- `provider.go` — the caller's own cancellation is a clean stop, recorded at warning under its own name and not retried. The transient classifier reads error types and message markers, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database. A cancellation that lands while an attempt waits out its backoff is recorded and marked the same way, so it does not travel up as a bare resolution failure for some later writer to file at error. Only `context.Canceled`: the ping budget derives from the connect timeout, so a deadline here can be the database itself

- `provider.go` — the connection-failure record carries the pool sizing and the deadlines that governed the attempt, the pgsql sibling's diagnostic shape; it named only the address that refused, so the same outage read differently depending on which provider reported it
- `provider.go` — every non-positive field of `PoolConfig` and `TimeoutConfig` falls back to the constructor default, the way the retry configuration already did. A zero reaches the provider far more often from an environment key nobody set than from a caller who means "no limit": on `database/sql` a zero maximum is an UNLIMITED pool and a zero lifetime means connections that are never recycled, while on this driver a zero read or write deadline means no deadline at all — so the zero-value configuration disarmed exactly the protections the nil configuration installs, and a negative one put the deadline in the past, failing every dial instantly with an i/o timeout no network event caused. **Behavioural**: a configuration that relied on zero meaning "unlimited" now receives the documented defaults; the migration connection keeps its deliberately lifted deadlines and disabled recycling
- `provider.go` — a nil logger no longer routes the retry loop's records to a discard sink while the terminal error still carries the already-logged mark, which suppressed the framework's own writers too and let a down database fail with zero diagnostic output anywhere; `OpenContext` now reads a nil logger — typed nil included — as the emergency logger, so the retry warnings and the terminal record reach standard error the way the first major always wrote them
- `provider.go` — an open with a nil logger no longer consumes the process-lifetime diagnostics routing with a logger that discards everything, which silenced bun's declaration warnings — unknown struct tag options, queries with arguments but no placeholders — for every later open in the process, the correctly wired ones included; the routing now receives the logger the retry loop logs to, which the nil fallback above guarantees is a sink that actually writes

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
