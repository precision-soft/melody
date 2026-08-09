# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ContextOpener`, the optional capability of opening under a caller's context, and `NewManagerRegistryWithContext`, which binds the registry to the context its lazy opens run under: a provider that implements the capability has its retry sleeps watch the context alongside the clock, so a shutdown that cancels it reaches a dial in flight instead of sleeping through the whole retry budget — the exact window in which supervisors send their signals. A nil context reads as `context.Background()`, the exact behaviour of `NewManagerRegistry`
- `MigrationProvider`, the optional capability of opening a connection tuned for migrations, and `ManagerRegistry.MigrationDatabase`, which answers the connection the migration commands should run on: the dedicated one with the driver deadlines lifted when the provider implements the capability, the ordinary pooled connection otherwise. The request pool carries read and write deadlines sized for requests, and a DDL statement that legitimately runs past them — an `ALTER TABLE` adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The dedicated database is opened once per name, cached beside the request pools — never inside them — closed by `Close`, and refused after it

### Fixed

- `manager_registry.go` — the lazy opens replay through the container behind the construction resolver, asked through the framework's new `ContainerCarrier` door, instead of through the resolution context that built the registry. The natural wiring — a container provider handing the registry the resolver it received — captured a single-threaded, scope-bound resolution context: a definition first dialed after the originating scope closed failed "scope is closed" forever over a healthy configuration, and two definitions dialed concurrently raced on the context's own state, reported by the race detector. Both measured and both inverted: the late dial resolves, the detector is silent. A caller that handed the container itself keeps identical behaviour. The registry's GoDoc now states its level out loud: it is a container-level service — a `*bun.DB` is a connection pool, the process-lifetime shape of `database/sql` — and per-unit work takes a transaction or a `Conn` from the pool, not a pool per scope
- the manager registry no longer wedges permanently when opening a database panics while the registry is being closed. The section that publishes the opened manager released its lock without a defer, so a panic from the database's own `Close` unwound with the lock held and the recovery path then blocked on that same lock — after which every later call to the registry blocked forever, silently. A provider returning a nil database was enough to trigger it
- `NewManagerRegistry` refuses a typed-nil resolver or provider. A nil concrete pointer boxed in the interface passed the plain nil comparison, so the boot-time refusal degraded into a panic at the first resolution, far from the wiring mistake that produced it
- a provider answering neither a database nor an error is refused with `ErrProviderReturnedNilDatabase` instead of being memoized as a manager wrapping nil, which handed callers a nil `*bun.DB` with no error and panicked at the first query; the refusal is not memoized, so a repaired provider is retried. Both open paths — `Manager` and `MigrationDatabase` — refuse it
- a database handed over beside a non-nil error is closed instead of leaked: the `Provider` contract does not promise a nil database with the error, and the registry was the last holder of that pool. Both open paths close it
- the error handed to callers coalesced onto a panicked open now carries the panic value in its context; they received an error naming only the definition while the actual reason rode a separate panic on the opening goroutine
- `Close` names every database that failed to close: with several failures, only the first was returned and the rest left no trace anywhere. A lone failure keeps its own error untouched
- `Manager` refuses everything after `Close` instead of only the names it had not opened yet. `Close` ends every pool it memoized without emptying the map, so a name already opened was answered with the manager over the now-dead pool and a nil error, while the same call for an unopened name was refused by `ErrManagerRegistryClosed` — one registry answering the same question two ways, with the answer that looked like success failing at the first query. The refusal also spares the dial: an unopened name used to be dialed in full, retry backoffs included, before the publish step refused it. **Behavioural**: `Manager`, `DefaultManager`, `Database` and their `Must` forms now refuse after `Close`

## [v1.0.1] - 2026-07-11 - Manager Registry Open Concurrency and Panic Safety

### Fixed

- `manager_registry.go` — a panic inside `Provider.Open` no longer wedges every later caller. `Manager` opens outside the registry lock and coalesces concurrent callers of the same name onto one open, so when a mis-parameterised provider panicked (the pgsql/mysql providers resolve parameters with `MustGet`) the deletion of the in-flight entry and the close of its done channel were skipped, and a recovered panic left every subsequent `Manager(name)` blocked forever on the never-closed channel. The open is now wrapped so the slot is always cleared and waiters are released with an error, while the panic still propagates to the direct caller.
- `manager_registry.go` — a database opened while `Close` is running is no longer leaked. `Close` drains the manager map without an open still in flight, so an open completing afterwards memoised its `*bun.DB` into a registry nobody would drain again; `Manager` now closes the freshly opened database and returns `ErrManagerRegistryClosed` when it finds the registry closed on completion.
- `manager_registry.go` — `ManagerRegistry` no longer holds its internal lock across `Provider.Open`. A slow or unreachable database otherwise serialized `Manager`/`Database`/`Close` for every other database and stalled shutdown; concurrent openers of the same manager now coalesce onto one open, and a failed open is not cached.

## [v1.0.0] - 2026-02-05 - Initial Release — Bun ORM Integration

### Added

- `provider.go` — `bunorm.Provider` — dialect-agnostic database provider interface
- `provider_definition.go` — `bunorm.ProviderDefinition` — registers multiple database providers with default-provider support
- `manager_registry.go` — `bunorm.ManagerRegistry` — caches and manages `*bunorm.Manager` instances (1:1 per provider definition); exposes `Manager(name)` / `MustManager(name)` / `DefaultManager()` / `MustDefaultManager()` / `DefaultDatabase()` / `MustDefaultDatabase()` accessors
- `manager.go` — `bunorm.Manager` — owns a single `*bun.DB`; exposes `Database()` and `Close()` methods
- `errors.go` — error sentinels: `ErrResolverIsRequired`, `ErrNoProviderDefinitions`, `ErrProviderDefinitionNameIsRequired`, `ErrProviderIsRequired`, `ErrProviderDefinitionNameMustBeUnique`, `ErrMultipleDefaultProviderDefinitions`
- `README.md` — service registration patterns

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v1.0.1...HEAD

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v1.0.0...integrations/bunorm/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v1.0.0
