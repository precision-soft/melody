# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- the migrate and rollback commands prefer the dedicated migration connection when the registry's provider implements `bunorm.MigrationProvider`, and fall back to the ordinary pool otherwise: the request pool carries driver deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement. The verbose manager label says which connection the run rides — "(dedicated migration connection)"
- the verbose DATABASE block is answered for PostgreSQL too, through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. It was a MySQL-only facility, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable. A connection over a unix socket, the one a local migration is most likely to use, reports its host as `<local socket>`

### Fixed

- the migration lock is released on a context detached from the command's own, so interrupting a running migration no longer leaves the lock row behind and refusing every later migration until someone runs the unlock command by hand. The cancelled context made the delete fail before it reached the database, and the failure was discarded; it is now reported
- a failed lock release fails the command. The failure was printed and then dropped, so a migrate or rollback whose unlock failed exited 0 while the lock row survived — a deploy script read success over a database that refuses every later migration on every replica. **Behavioural**: the exit code now reports it; a migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it
- `RunQueries` and `RunQueriesWithOption` report an empty query set as a warning instead of printing "all 0 queries executed successfully". The run still succeeds — the caller decides what an empty migration means — but under `WithMarkAppliedOnSuccess(true)` the migration is marked applied and never runs again, so a builder that produced nothing must not read like the queries ran
- `RegisterCommands` refuses a nil migration set instead of returning no commands at all. The silence left a direct caller believing it had registered the command family and finding out at invocation time, as "unknown command", far from the wiring that caused it; `Module` gates its own optional set before calling, so a binary registering only migration contexts is unaffected. **Behavioural**: `RegisterCommands(nil, ...)` now panics at registration
- table cells are truncated by runes, not bytes: the format widths pad by rune count, so a multi-byte value was truncated even when it fit the column, with the cut landing mid-rune and rendering as a replacement character; a budget too small for the ellipsis no longer slices negative

## [v1.2.0] - 2026-07-11 - Multi-Context Migration Command Families

### Added

- `context.go`, `register.go`, `module.go` — multi-context migrations for a binary with several databases: `ModuleConfig` gains `Contexts []ContextConfig{Name, Migrations, Options}`, and one module registration now generates a full per-context command family — `db:<name>:init|migrate|rollback|status|unlock|create` — instead of the module being registered once per database with hand-managed prefixes and separate registries. Each context resolves against the one shared `*bunorm.ManagerRegistry` and is pinned to its manager (by convention the manager name equals the context name; the command prefix derives as `<basePrefix>:<name>`), and the plain `RegisterContextCommands(contexts, baseOptions)` form exists for hand-wired setups. The legacy single-set form is untouched and composes with contexts in the same `ModuleConfig`. Back-port from `v3`.
- `option.go` — `Options.ManagerName` pins the registry manager a command set uses when the `--manager` flag is absent, replacing the fall-through to the registry default. Resolution order: `--manager` flag, then the pin, then `registry.DefaultManager()`. This closes the multi-context foot-gun where omitting the flag silently migrated whichever context happened to be the shared registry's default. Back-port from `v3`.

### Changed

- `context.go` — a migration context's `ManagerName` no longer inherits the base `Options` pin; a context targets the manager named after it (its own database) unless it sets its own `Options.ManagerName`. Reusing one base `Options{ManagerName}` (via `NewModule`'s shared `Options`, say) could otherwise silently migrate every context against that one manager's database. The other fields — `ManagerRegistryServiceId`, `ManagerFlagName`, `CommandPrefix` — still cascade from the base options. Aligned with `v3`.

## [v1.1.1] - 2026-06-25 - Clean Error on Unknown Migration Manager Name

### Fixed

- `base_command.go` — `resolveDatabase` resolved a named manager with `registry.MustManager`, which panics (`exception.Panic`) when the `--manager` flag names a manager that is not registered, so any migration command (`db:migrate`, `db:rollback`, `db:status`, …) invoked with an unknown manager name aborted with an uncaught panic instead of the clean "provider definition not found" error. The named-manager branch now calls `registry.Manager` and returns the error, matching the sibling default-manager branch and the `v2`/`v3` behavior. Ported from the `v2`/`v3` fix.

## [v1.1.0] - 2026-06-16 - Lock Concurrent Migrations and Plug-and-Play Module Registration

### Added

- `module.go` — `migrate.NewModule(ModuleConfig{Migrations, Options})` self-registering application module that registers the `db:*` migration commands, so `app.RegisterModule(migrate.NewModule(...))` replaces a hand-written `RegisterCommands` call into the application's `RegisterCliCommands`.

### Fixed

- `command_migrate.go`, `command_rollback.go` — `db:migrate`/`db:rollback` now take the bun migration lock (`migrator.Lock`/`Unlock`) around the run, so two replicas running the command during a rolling deploy cannot both compute the same pending set and double-apply a migration. Ported from the `v3` fix.

## [v1.0.0] - 2026-02-06 - Initial Release — Programmatic Migration Helpers

### Added

- `query.go` — `migrate.Query` — `Name` + `SQL` pair describing a migration step
- `option.go` — `migrate.RunnerOption` — configures output writer and color support; `DefaultRunnerOption()` returns stdout + color enabled
- `migrate.go` — `migrate.RunQueries(ctx, db, direction, migrationName, queries)` — executes a batch of migration steps; `RunQueriesWithOption()` variant accepting `RunnerOption`
- `migrate.go` — `migrate.Up()` / `UpWithOption()` — forward-migration convenience; `Down()` / `DownWithOption()` — rollback convenience
- `README.md` — migration workflow documentation; CLI commands introduced in the v2 binding

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.2.0...HEAD

[v1.2.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.1.1...integrations/bunorm/migrate/v1.2.0

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.1.0...integrations/bunorm/migrate/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.0.0...integrations/bunorm/migrate/v1.1.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v1.0.0
