# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.1.1...HEAD

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.1.0...integrations/bunorm/migrate/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.0.0...integrations/bunorm/migrate/v1.1.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v1.0.0
