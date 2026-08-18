# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- under `--format=json` every command renders the one machine-readable document the declared flag always promised: the accumulated blocks as data, the side-reports as warnings, the failure inside the envelope's error. The flag was declared and validated by `StandardFlags` long before it was honoured — `db:status --format=json | jq` failed on the first byte of a plain-text table while the cli runner had already silenced its banner on the json promise
- `SetDefaultRunnerOption` — the migrate and rollback commands install their parsed writer and colour posture as the process default `RunQueries` reads when a migration passes no option of its own: a generated migration's signature is fixed by bun as `(ctx, db)` and cannot receive the parsed flags any other way, so a `--no-color` run carried escape codes from exactly the per-query lines the flag was passed to clean. Under json the per-query progress is discarded — the document is the only byte the command may emit
- every one of the six commands prefers the dedicated migration connection when the registry's provider implements `bunorm.MigrationProvider`, and falls back to the ordinary pool otherwise — they share one manager resolution, so the preference is not per command: the request pool carries driver deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement. The verbose manager label says which connection the run rides — "(dedicated migration connection)"
- the verbose DATABASE block is answered for PostgreSQL too, through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. It was a MySQL-only facility, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable. A connection over a unix socket, the one a local migration is most likely to use, reports its host as `<local socket>`

### Changed

- the json envelope is one line, following the framework's printer: `db:status --format=json` and its siblings render a single closed document per run rather than an indented block, and `--format=json-pretty` renders the same document indented for reading by hand. Nothing that decodes the document is affected

- the commands no longer pre-print the failure they return: the cli runner's `[error]` line and the full log record already report it, and the third copy of the same message on the same console said nothing new. The deliberate exception stays — an unlock failure beside a failed migration is still printed, because the return keeps the migration's error and would lose it

### Fixed

- the package's shared test material moves to `fixture_test.go`, and `control_character.go` gains the mirror it never had. The doubles the whole suite is built on — the fake connector, driver, connection, rows and dialect, the query recorder, the runtime and command helpers — lived inside `migrate_test.go` beside that file's own ten tests, so seven other files reached into a file whose name says it tests one source. The escaping helper that renders text of server origin for an operator's terminal had no test at all; it has one now, covering the named escapes, the hexadecimal ones, DEL, and the multi-line form the query rendering uses

- documentation: the readme states that without the `--manager` flag the registration's own `ManagerName` pin is used, and only an empty pin falls back to the registry default. The dedicated migration connection is attributed to all six commands rather than to migrate and rollback, since they share one manager resolution

- the unknown-manager test pins the refusal it always meant to pin: it accepted any error, so a regression that ignored the `--manager` flag and dialed the default database still read as the guard working; it now requires `bunorm.ErrProviderDefinitionNotFound`

- **Behavioural:** the text rendering escapes C0 control characters and DEL visibly (`\n`, `\r`, `\t`, the rest as `\xNN`) in every string it did not write itself — the error text off the wire, the failed statement, the query names, the DATABASE identity block the server answers and the migration names — before the terminal sees them, and before the table cells are measured, so the alignment counts the escaped spelling. A server whose error carried an escape sequence could repaint the operator's terminal or forge lines in a captured log; the failed statement alone keeps its real line breaks, which are the readability of the query block, with every other control byte escaped. The json rendering is untouched — its encoder escapes on its own
- `db:rollback` answers the held migration lock with the same remedy-naming refusal `db:migrate` already answered: which manager, which locks table, and that `db:unlock` clears a lock a crashed process left behind. It used to return bun's bare error, which states that a lock exists and nothing else; the bun error stays the cause, so `errors.Is` still reaches it

- the refusal of a held migration lock names the resource and the remedy. Bun's own error states that a lock exists and nothing else — not which database it belongs to, and not that this command set ships `<prefix>:unlock` to clear one a crashed process left behind — and it reached the operator carrying no melody context at all, while the unknown-manager refusal three lines away has always named both what was asked for and what exists. The manager label, the lock table and the unlock command travel in the context now, with bun's error kept as the cause so `errors.Is` still reaches it. **Behavioural change** for anything matching on the message text — see the framework's `UPGRADE.md`
- a migration group that fails part way through names the migrations that already landed, on the plain text and in the machine document alike. Bun returns them beside the failure and the command threw them away, so the operator was told which migration broke and nothing about which had been applied and recorded — leaving the choice between re-running (safe) and rolling back (which would take the landed ones with it) impossible to make without reading the database by hand, and `data` was `{}` for a run that had applied two migrations. The last entry of bun's group is the migration that failed rather than one that succeeded — the group is fixed to the slice up to and including the one about to be attempted, before it runs — so it is counted out, which matches the migrations table exactly, since the migrator marks a migration applied only once its `Up` returns. A run that landed nothing reports no applied block at all rather than an empty one claiming a partial state
- `db:migrate` prints a success line on the run that changed the schema. The line lived inside `wantsDetail()`, so a plain run — the shape a deploy script invokes — printed `WARNING: no pending migrations` for the run that did nothing and not one byte for the run that applied five: the log was empty exactly when something had happened, and the operator reading it at three in the morning could not tell the two apart, while the `db:rollback` sibling has always printed its own. The line names the applied count, the manager label the run rode and the group; the name list stays behind `--verbose` and the json document is deliberately unchanged, since under it the same run already carries all three as structured fields
- the json envelope carries the failure's own context under `error.details` and the whole chain under `error.cause.details.chain`. Both were `nil` on every failure alike, so the machine document — the contract a pipeline reads — was the single rendering that threw away what the error already carried: a wrong password answered one sentence beside `"details":null,"cause":null` on stdout while the journal, over the same value at the same instant, filed the connection, the pool sizing, the deadlines and the driver's own refusal. The details object stays an object when the failure carries no context, so the field keeps its json type on every failure
- a migration read from a `.sql` file whose statement the database refuses now fails the command instead of being reported as applied. The module required `bun v1.2.16`, where the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec failure with its own nil return, so `Migrate` answered nil for a statement that never ran: `db:migrate` printed `[success]`, exited 0, wrote nothing to the journal, and marked the migration applied — which made the failure unrepeatable and `db:status` report it as done. The requirement moves to `bun v1.2.17`, the oldest release that returns the exec failure, and a regression pin drives a refused `.up.sql` through the migrator built here and requires both halves: the refusal reaches the caller, and no row is written for it. Go migrations were never affected
- the migration lock is released on a context detached from the command's own, so interrupting a running migration no longer leaves the lock row behind and refusing every later migration until someone runs the unlock command by hand. The cancelled context made the delete fail before it reached the database, and the failure was discarded; it is now reported
- a failed lock release fails the command. The failure was printed and then dropped, so a migrate or rollback whose unlock failed exited 0 while the lock row survived — a deploy script read success over a database that refuses every later migration on every replica. **Behavioural**: the exit code now reports it; a migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it
- `RunQueries` and `RunQueriesWithOption` report an empty query set as a warning instead of printing "all 0 queries executed successfully". The run still succeeds — the caller decides what an empty migration means — but under `WithMarkAppliedOnSuccess(true)` the migration is marked applied and never runs again, so a builder that produced nothing must not read like the queries ran
- `RegisterCommands` refuses a nil migration set instead of returning no commands at all. The silence left a direct caller believing it had registered the command family and finding out at invocation time, as "unknown command", far from the wiring that caused it; `Module` gates its own optional set before calling, so a binary registering only migration contexts is unaffected. **Behavioural**: `RegisterCommands(nil, ...)` now panics at registration
- table cells are truncated by runes, not bytes: the format widths pad by rune count, so a multi-byte value was truncated even when it fit the column, with the cut landing mid-rune and rendering as a replacement character; a budget too small for the ellipsis no longer slices negative

- README — the flags section no longer claims `--format=json` is unimplemented: under it every command accumulates its output and renders the one machine-readable json envelope, failure included

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
