# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- the migration commands run on the dedicated migration connection when the provider offers one (`bunorm.MigrationProvider`), falling back to the ordinary pool otherwise; the resolved database label says `(dedicated migration connection)` so the run reports which connection carried it. A long migration — an ALTER TABLE adding cascade foreign keys on a large table — used to be cut by the request pool's 30s driver deadlines with `invalid connection` mid-sequence, where MySQL DDL leaves partially applied steps behind

### Fixed

- a migration read from a `.sql` file whose statement the database refuses now fails the command instead of being reported as applied. The module required `bun v1.2.16`, where the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec failure with its own nil return, so `Migrate` answered nil for a statement that never ran: `db:migrate` printed `[success]`, exited 0, wrote nothing to the journal, and marked the migration applied — which made the failure unrepeatable and `db:status` report it as done. The requirement moves to `bun v1.2.17`, the oldest release that returns the exec failure, and a regression pin drives a refused `.up.sql` through the migrator built here and requires both halves: the refusal reaches the caller, and no row is written for it. Go migrations were never affected
- the migration lock is released on a context detached from the command's own, so interrupting a running migration no longer leaves the lock row behind and refusing every later migration until someone runs the unlock command by hand. The cancelled context made the delete fail before it reached the database, and the failure was discarded; it is now reported

## [v3.1.0] - 2026-07-11 - Multi-Context Migrations and Per-Context Manager Pinning

### Added

- `context.go`, `register.go`, `module.go` — multi-context migrations for a binary with several databases: `ModuleConfig` gains `Contexts []ContextConfig{Name, Migrations, Options}`, and one module registration now generates a full per-context command family — `db:<name>:init|migrate|rollback|status|unlock|create` — instead of the module being registered once per database with hand-managed prefixes, separate registries and `container.WithoutTypeRegistration()`. Each context resolves against the one shared `*bunorm.ManagerRegistry` and is pinned to its manager (by convention the manager name equals the context name; the command prefix derives as `<basePrefix>:<name>`), and the plain `RegisterContextCommands(contexts, baseOptions)` form exists for hand-wired setups. The legacy single-set form is untouched and composes with contexts in the same `ModuleConfig`.
- `option.go` — `Options.ManagerName` pins the registry manager a command set uses when the `--manager` flag is absent, replacing the fall-through to the registry default. Resolution order: `--manager` flag, then the pin, then `registry.DefaultManager()`. This closes the multi-context foot-gun where omitting the flag silently migrated whichever context happened to be the shared registry's default.

### Changed

- `context.go` — a migration context's `ManagerName` no longer inherits the base `Options` pin; a context targets the manager named after it (its own database) unless it sets its own `Options.ManagerName`. Reusing one base `Options{ManagerName}` (via `NewModule`'s shared `Options`, say) could otherwise silently migrate every context against that one manager's database. The other fields — `ManagerRegistryServiceId`, `ManagerFlagName`, `CommandPrefix` — still cascade from the base options.

## [v3.0.3] - 2026-06-16

### Added

- `v3/README.md` — added a v3 module README documenting `RegisterCommands`, the `Options` defaults, the `CliModule` wiring, and the generated `db:*` migration commands.
- `v3/module.go` — `migrate.NewModule(ModuleConfig{Migrations, Options})` self-registering application module that registers the migration commands, so `app.RegisterModule(migrate.NewModule(...))` replaces a hand-written `RegisterCommands` call into the application's `RegisterCliCommands`. v3 binding.

### Fixed

- `context.go` — a base-level `Options.ManagerName` pin is inherited by every context, as its own doc and each sibling field already promised. An empty per-context `ManagerName` jumped straight to the context name, so the pin was silently ignored and each context's commands targeted a manager merely named after it.
- `v3/base_command.go` — every `db:*` migration command (`init`/`migrate`/`rollback`/`status`/`unlock`/`create`) now returns a clean error instead of panicking when the `--manager` flag names a manager that is not registered (a typo) or whose database fails to open. `resolveDatabase` resolved a named manager through the panicking `registry.MustManager`, so an unknown name crashed the CLI with a stack trace rather than a `printError` message and a non-zero exit; it now uses the error-returning `registry.Manager`, matching the default-manager branch.
- `v3/command_migrate.go`, `v3/command_rollback.go` — `db:migrate` and `db:rollback` now take the bun migration lock (`migrator.Lock`/`Unlock`) around the run. bun's `Migrator.Migrate`/`Rollback` do no locking of their own, so two replicas running the command during a rolling deploy both computed the same pending set and double-applied a migration (the module ships `db:unlock` for exactly this lock, but nothing ever acquired it). The lock serializes concurrent runs; the lock table is created by `db:init`.

## [v3.0.2] - 2026-03-08 - Fix Stale bunorm/v2 Import in v3

### Fixed

- `v3/base_command.go` — import corrected from `github.com/precision-soft/melody/integrations/bunorm/v2` to `/v3` (stale v2 import accidentally carried over from the v3.0.0 cut)
- `v3/go.mod` — `bunorm` dependency bumped from v2.0.0 to v3.0.1; indirect `melody/v2` dependency removed
- `v3/go.sum` — regenerated after dependency correction

## [v3.0.1] - 2026-03-08 - Tidy v2 and v3 Dependencies

### Changed

- `v2/base_command.go`, `v2/go.mod`, `v2/go.sum` — dependency fixes; note: v3 still carried a stale `bunorm/v2` import at this point, fully corrected in v3.0.2
- `v3/base_command.go`, `v3/go.mod`, `v3/go.sum` — dependency fixes (stale import present, see v3.0.2)

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/migrate/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/migrate/v3/`; v2 and v3 implementations maintained in parallel
- `go.mod` — dependencies: `github.com/precision-soft/melody/integrations/bunorm/v3 v3.0.0`, `github.com/precision-soft/melody/v3 v3.0.0`

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.3...integrations/bunorm/migrate/v3.1.0

[v3.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.2...integrations/bunorm/migrate/v3.0.3

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v3.0.0
