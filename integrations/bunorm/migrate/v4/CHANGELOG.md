# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Behavioural change** — `command_migrate.go`, `option.go`: a `db:migrate` whose last migration ran but was not recorded writes a marker row, `ranNotRecorded:<migrations table>:<name>`, in the locks table, and while a marker stands the next `db:migrate` is refused before any `Up`, naming the migrations and the flag; `--force` (`MigrateForceFlagName`) deletes the markers and migrates. The marker is written detached from the command's context, under `CommandConfig.UnlockTimeout`, so a stop cancelling the run between the `Up` and the mark still leaves it; a marker that cannot be written is said in a warning.
- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `option.go`: `CommandConfig.UnlockTimeout`, the budget of the migration lock's release after a run and of a ran-but-not-recorded marker's write, 5s when left out as the constant was, at least 1ms, unlimited refused, a value outside the rule panicking at registration naming the field; a migration context inherits the base config's value unless its own `Command` sets one.
- `option.go`: `DefaultManagerRegistryServiceId`, `DefaultManagerFlagName` and `DefaultCommandPrefix`, the defaults a `CommandConfig` field takes when left out.
- `parameter.go`, `module.go`: the keys `MELODY_BUNORM_MIGRATE_MIGRATIONS_TABLE` and `MELODY_BUNORM_MIGRATE_LOCKS_TABLE` name the two tables bun keeps, `bun_migrations` and `bun_migration_locks` when unset, one pair for every command of the process; the module declares them, `ModuleConfig.KeyDefaults` their defaults, `EnvironmentKeys(defaults)` for a host that builds the commands without it; a value that is not a plain identifier of at most 64 bytes fails the boot, the error's context naming the key, two keys naming one table are refused at the run before the database is opened, and every message that names a table names the configured one.

### Changed

- **Breaking** — `migrate.go`, `option.go`, `context.go`, `module.go`, `README.md`: `RunnerOption` is `RunnerOptions`, the struct `RunQueries`, `Up` and `Down` take per call, and `Options`, the struct the command constructors, `RegisterCommands` and `RegisterContextCommands` take, is `CommandConfig`; `ContextConfig.Options` and `ModuleConfig.Options` are `Command`; the unlock budget's refusal names `CommandConfig.UnlockTimeout`. No field and no behaviour changed.
- **Breaking** — `option.go`: `DefaultManagerRegistryServiceId` is `service.bunorm.manager_registry`, where it was `service.database.manager.registry`, the container's one service-name grammar; a registry registered under the old name is named through `ManagerRegistryServiceId` or moved to the new one.
- **Breaking** with the framework: the fourth major's runtime is read from `v4/container/contract` and built through `v4/container/runtime`, a runtime resolving through its scope only; `runtime.New` and `Runtime.Container()` are gone. See the framework's `UPGRADE.md`.
- `base_command.go`, `command_create.go`: the migration commands resolve the manager registry under their run's context, the fourth major's resolver form.
- **Behavioural change** — `base_command.go`: the migration commands hand their run's runtime to the registry's `MigrationDatabase`, so a cancelled run refuses before the dial; every run is on the provider's migration connection, labelled `(dedicated migration connection)`, and releases its hold through `CloseMigrationDatabase`, which leaves a pool the registry shares by identity open.
- **Breaking** — `migrate.go`: `RunQueries`, `Up` and `Down` take a `RunnerOptions` last; `RunQueriesWithOption`, `UpWithOption` and `DownWithOption` are removed.
- **Breaking** — `migrate.go`, `command_migrate.go`, `command_rollback.go`: `DefaultRunnerOption` and `SetDefaultRunnerOption` are removed with the process-wide fallback they fed, which the migrate and rollback commands installed for the length of their run; the context the migrator hands every migration is the one channel from a command to its migrations, and a migration that drops it prints to standard output.
- **Breaking** — `option.go`: `DefaultOptions()` is removed; `CommandConfig{}` takes every default at `RegisterCommands` and at every command constructor, where a constructor called directly kept its empty strings.
- **Behavioural change** — `migrate.go`: `RunnerOptions{}` prints as the running command prints, a nil `Writer` the command's writer, `NoColor` false the command's colour, where a zero option handed to the `WithOption` doors forced colour under `--no-color`; `NoColor: true` still prints plain.

- `output.go`: the log records are built through `exception.LogContext(err, exception.LogContextOptions{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `migrate.go`: `Query.SQL` is renamed `Query.Sql`; nothing else changes.
- **Breaking** — `command_init.go`: `InitCommand` and `NewInitCommand` are renamed `InitializeCommand` and `NewInitializeCommand`; the command keeps its name.

### Fixed

- `output.go`: the name column of a migrations listing follows the longest name, counted in runes, bounded by `Options.TableMaxWidth`, a longer name wrapping onto continuation rows, where every name was cut at 46 characters and two names sharing their first 46 printed alike.
- `output.go`: the DETAILS and DATABASE blocks follow the same rule, the value column fitting the longest value, bounded by `Options.TableMaxWidth`, a longer value wrapping onto rows under an empty key, where a migration name was cut at 46 characters and a host, a user or a version at 15.
- output: a migration's error whose `Unwrap` chain reaches itself no longer holds the command that renders it.

