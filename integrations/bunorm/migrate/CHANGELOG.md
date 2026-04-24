# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.2...HEAD

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v2.0.0...integrations/bunorm/migrate/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v1.0.0...integrations/bunorm/migrate/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Fbunorm%2Fmigrate%2Fv1.0.0
