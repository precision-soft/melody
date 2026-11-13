# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `option.go`: `Options.UnlockTimeout`, the budget of the migration lock's release after a run, 5s when left out as the constant was, at least 1ms, unlimited refused, a value outside the rule panicking at registration naming the field; a migration context inherits the base options' value unless its own options set one.
- `option.go`: `DefaultManagerRegistryServiceId`, `DefaultManagerFlagName` and `DefaultCommandPrefix`, the defaults an `Options` field takes when left out.

### Changed

- `base_command.go`, `command_create.go`: the migration commands resolve the manager registry under their run's context, the fourth major's resolver form.
- **Breaking** — `migrate.go`: `RunQueries`, `Up` and `Down` take a `RunnerOption` last; `RunQueriesWithOption`, `UpWithOption` and `DownWithOption` are removed.
- **Breaking** — `migrate.go`, `command_migrate.go`, `command_rollback.go`: `DefaultRunnerOption` and `SetDefaultRunnerOption` are removed with the process-wide fallback they fed, which the migrate and rollback commands installed for the length of their run; the context the migrator hands every migration is the one channel from a command to its migrations, and a migration that drops it prints to standard output.
- **Breaking** — `option.go`: `DefaultOptions()` is removed; `Options{}` takes every default at `RegisterCommands` and at every command constructor, where a constructor called directly kept its empty strings.
- **Behavioural change** — `migrate.go`: `RunnerOption{}` prints as the running command prints, a nil `Writer` the command's writer, `NoColor` false the command's colour, where a zero option handed to the `WithOption` doors forced colour under `--no-color`; `NoColor: true` still prints plain.

- `output.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `migrate.go`: `Query.SQL` is renamed `Query.Sql`; nothing else changes.

### Fixed

- output: a migration's error whose `Unwrap` chain reaches itself no longer holds the command that renders it.

