# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.1.0] - 2026-06-16 - Return a Clean Error for an Unknown --manager, Lock Concurrent Migrations, and Plug-and-Play Module Registration

### Added

- `v2/module.go` — `migrate.NewModule(ModuleConfig{Migrations, Options})` self-registering application module that registers the `db:*` migration commands, so `app.RegisterModule(migrate.NewModule(...))` replaces a hand-written `RegisterCommands` call into the application's `RegisterCliCommands`.

### Fixed

- `v2/base_command.go` — every `db:*` migration command now returns a clean error instead of panicking when the `--manager` flag names an unregistered or un-openable manager; `resolveDatabase` now uses the error-returning `registry.Manager` rather than the panicking `registry.MustManager`. Ported from the `v3` fix.
- `v2/command_migrate.go`, `v2/command_rollback.go` — `db:migrate`/`db:rollback` now take the bun migration lock (`migrator.Lock`/`Unlock`) around the run, so two replicas running the command during a rolling deploy cannot both compute the same pending set and double-apply a migration. Ported from the `v3` fix.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.1.0...HEAD

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.0.0...integrations/bunorm/migrate/v2.1.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v2.0.0
