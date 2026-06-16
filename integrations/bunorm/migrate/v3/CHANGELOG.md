# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.3] - 2026-06-16

### Added

- `v3/README.md` — added a v3 module README documenting `RegisterCommands`, the `Options` defaults, the `CliModule` wiring, and the generated `db:*` migration commands.
- `v3/module.go` — `migrate.NewModule(ModuleConfig{Migrations, Options})` self-registering application module that registers the migration commands, so `app.RegisterModule(migrate.NewModule(...))` replaces a hand-written `RegisterCommands` call into the application's `RegisterCliCommands`. v3 binding.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.3...HEAD

[v3.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.2...integrations/bunorm/migrate/v3.0.3

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v3.0.0
