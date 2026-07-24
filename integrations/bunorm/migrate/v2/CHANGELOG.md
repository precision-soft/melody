# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.2.1] - 2026-07-25 - Migration Lock Release on an Interrupted Run

### Fixed

- the migration lock is released on a context detached from the command's own, so interrupting a running migration no longer leaves the lock row behind and refusing every later migration until someone runs the unlock command by hand. The cancelled context made the delete fail before it reached the database, and the failure was discarded; it is now reported

## [v2.2.0] - 2026-07-11 - Multi-Context Migration Command Families

### Added

- `v2/context.go`, `v2/register.go`, `v2/module.go` — multi-context migrations for a binary with several databases: `ModuleConfig` gains `Contexts []ContextConfig{Name, Migrations, Options}`, and one module registration now generates a full per-context command family — `db:<name>:init|migrate|rollback|status|unlock|create` — instead of the module being registered once per database with hand-managed prefixes, separate registries and `container.WithoutTypeRegistration()`. Each context resolves against the one shared `*bunorm.ManagerRegistry` and is pinned to its manager (by convention the manager name equals the context name; the command prefix derives as `<basePrefix>:<name>`), and the plain `RegisterContextCommands(contexts, baseOptions)` form exists for hand-wired setups. The legacy single-set form is untouched and composes with contexts in the same `ModuleConfig`.
- `v2/option.go` — `Options.ManagerName` pins the registry manager a command set uses when the `--manager` flag is absent, replacing the fall-through to the registry default. Resolution order: `--manager` flag, then the pin, then `registry.DefaultManager()`. This closes the multi-context foot-gun where omitting the flag silently migrated whichever context happened to be the shared registry's default.

### Changed

- `v2/context.go` — a migration context's `ManagerName` no longer inherits the base `Options` pin; a context targets the manager named after it (its own database) unless it sets its own `Options.ManagerName`. Reusing one base `Options{ManagerName}` (via `NewModule`'s shared `Options`, say) could otherwise silently migrate every context against that one manager's database. The other fields — `ManagerRegistryServiceId`, `ManagerFlagName`, `CommandPrefix` — still cascade from the base options. Aligned with `v3`.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.2.1...HEAD

[v2.2.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.2.0...integrations/bunorm/migrate/v2.2.1

[v2.2.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.1.0...integrations/bunorm/migrate/v2.2.0

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.0.0...integrations/bunorm/migrate/v2.1.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v2.0.0
