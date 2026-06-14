# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `command_migrate.go`, `command_rollback.go` (v1 and v2 modules) — `db:migrate`/`db:rollback` now take the bun migration lock (`migrator.Lock`/`Unlock`) around the run, so two replicas running the command during a rolling deploy cannot both compute the same pending set and double-apply a migration. Ported from the `v3` fix; no v1/v2 tag is cut for this change.

## [v3.0.3] - 2026-06-15

### Added

- `v3/README.md` — added a v3 module README documenting `RegisterCommands`, the `Options` defaults, the `CliModule` wiring, and the generated `db:*` migration commands.

### Fixed

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

## [v2.0.1] - 2026-06-11 - Return a Clean Error for an Unknown --manager

### Fixed

- `v2/base_command.go` — every `db:*` migration command now returns a clean error instead of panicking when the `--manager` flag names an unregistered or un-openable manager; `resolveDatabase` now uses the error-returning `registry.Manager` rather than the panicking `registry.MustManager`. Ported from the `v3` fix.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and CLI Command Integration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/migrate/v2` — Go v2 migration

### Changed

- Code migrated to `integrations/bunorm/migrate/v2/` with matching module path
- `go.mod` — dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`
- Programmatic API from v1 retained as a subset of the new CLI surface

### Added

- `register.go` — `Register()` function that wires migration commands into a Melody CLI application
- `database_identity.go` — `DatabaseIdentity` type — identifies which manager/database to migrate against
- `migrate.go` — `Migrate` type — orchestrates migrations, resolves named managers through `bunorm.ManagerRegistry`
- `command_create.go`, `command_init.go`, `command_migrate.go`, `command_rollback.go`, `command_status.go`, `command_unlock.go` — CLI migration commands
- `base_command.go` — `BaseCommand` — shared resolver-based manager lookup and error handling
- `option.go` — `Option` — builder for runner output/color customization; `WithOption()` variants of `Migrate` methods

## [v1.0.0] - 2026-02-06 - Initial Release — Programmatic Migration Helpers

### Added

- `query.go` — `migrate.Query` — `Name` + `SQL` pair describing a migration step
- `option.go` — `migrate.RunnerOption` — configures output writer and color support; `DefaultRunnerOption()` returns stdout + color enabled
- `migrate.go` — `migrate.RunQueries(ctx, db, direction, migrationName, queries)` — executes a batch of migration steps; `RunQueriesWithOption()` variant accepting `RunnerOption`
- `migrate.go` — `migrate.Up()` / `UpWithOption()` — forward-migration convenience; `Down()` / `DownWithOption()` — rollback convenience
- `README.md` — migration workflow documentation; CLI commands introduced in v2.0.0

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.3...HEAD

[v3.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.2...integrations/bunorm/migrate/v3.0.3

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v3.0.0

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.0.0...integrations/bunorm/migrate/v2.0.1
[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v1.0.0
