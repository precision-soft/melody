# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v3.0.2] - 2026-03-08

### Fixed

- `v3/base_command.go` — import corrected from `github.com/precision-soft/melody/integrations/bunorm/v2` to `/v3` (stale v2 import accidentally carried over from the v3.0.0 cut)
- `v3/go.mod` — `bunorm` dependency bumped from v2.0.0 to v3.0.1; indirect `melody/v2` dependency removed
- `v3/go.sum` regenerated accordingly

## [v3.0.1] - 2026-03-08

### Changed

- Patch release — `v2/base_command.go`, `v2/go.mod`, `v2/go.sum`, `v3/base_command.go`, `v3/go.mod`, `v3/go.sum` updated with dependency fixes
- Note: v3/ still carried a stale `bunorm/v2` import — fully corrected in v3.0.2

## [v3.0.0] - 2026-03-08

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/migrate/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/migrate/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v3 v3.0.0`, `github.com/precision-soft/melody/v3 v3.0.0`

## [v2.0.0] - 2026-02-17

### Breaking Changes

- Module path changed to `github.com/precision-soft/melody/integrations/bunorm/migrate/v2` — Go v2 migration
- Programmatic API from v1 retained; new CLI is a superset

### Added

- CLI command integration — `Register()` function that wires migration commands into a Melody CLI application
- `DatabaseIdentity` type — identifies which manager/database to migrate against
- `Migrate` type — orchestrates migrations, resolves named managers through `bunorm.ManagerRegistry`
- Commands: `CreateCommand`, `InitCommand`, `MigrateCommand`, `RollbackCommand`, `StatusCommand`, `UnlockCommand`
- `BaseCommand` — shared resolver-based manager lookup and error handling
- `Option` — builder for runner output/color customization; `WithOption()` variants of `Migrate` methods

### Changed

- Code migrated to `integrations/bunorm/migrate/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

## [v1.0.0] - 2026-02-06

### Added

- Initial release — programmatic helpers for running Bun SQL migrations
- `migrate.Query` — `Name` + `SQL` pair describing a migration step
- `migrate.RunnerOption` — configures output writer and color support; `DefaultRunnerOption()` returns stdout + color enabled
- `migrate.RunQueries(ctx, db, direction, migrationName, queries)` — executes a batch of migration steps
- `migrate.RunQueriesWithOption()` — variant accepting `RunnerOption`
- `migrate.Up()` / `UpWithOption()` — forward-migration convenience
- `migrate.Down()` / `DownWithOption()` — rollback convenience
- `migrationPrinter` — formats migration progress with colors and status messages
- README with migration workflow documentation

### Scope

Programmatic migration utilities only — CLI commands were introduced in v2.0.0.

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.0.0...integrations/bunorm/migrate/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.0.0...integrations/bunorm/migrate/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fmigrate%2Fv1.0.0
