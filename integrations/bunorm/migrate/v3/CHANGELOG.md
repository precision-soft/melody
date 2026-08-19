# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- under `--format=json` every command renders the one machine-readable document the declared flag always promised: the accumulated blocks as data, the side-reports as warnings, the failure inside the envelope's error. The flag was declared and validated by `StandardFlags` long before it was honoured — `db:status --format=json | jq` failed on the first byte of a plain-text table while the cli runner had already silenced its banner on the json promise
- `SetDefaultRunnerOption` — the migrate and rollback commands install their parsed writer and colour posture as the process default `RunQueries` reads when a migration passes no option of its own: a generated migration's signature is fixed by bun as `(ctx, db)` and cannot receive the parsed flags any other way, so a `--no-color` run carried escape codes from exactly the per-query lines the flag was passed to clean. Under json the per-query progress is discarded — the document is the only byte the command may emit
- the verbose DATABASE block is answered for PostgreSQL too, through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. It was a MySQL-only facility, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable. A connection over a unix socket, the one a local migration is most likely to use, reports its host as `<local socket>`

### Changed

- the migration commands run on the dedicated migration connection when the provider offers one (`bunorm.MigrationProvider`), falling back to the ordinary pool otherwise; the resolved database label says `(dedicated migration connection)` so the run reports which connection carried it. A long migration — an ALTER TABLE adding cascade foreign keys on a large table — used to be cut by the request pool's 30s driver deadlines with `invalid connection` mid-sequence, where MySQL DDL leaves partially applied steps behind

- the json envelope is one line, following the framework's printer: `db:status --format=json` and its siblings render a single closed document per run rather than an indented block, and `--format=json-pretty` renders the same document indented for reading by hand. Nothing that decodes the document is affected
- the commands no longer pre-print the failure they return: the cli runner's `[error]` line and the full log record already report it, and the third copy of the same message on the same console said nothing new. The deliberate exception stays — an unlock failure beside a failed migration is still printed, because the return keeps the migration's error and would lose it

### Fixed

- the package's shared test material moves to `fixture_test.go`, and `control_character.go` arrives with the mirror it never had. The doubles the whole suite is built on — the fake connector, driver, connection, rows and dialect, the query recorder, the runtime and command helpers — lived inside `migrate_test.go` beside that file's own ten tests, so seven other files reached into a file whose name says it tests one source
- the runner's own per-query progress is pinned out of the machine document: under `--format=json` the command installs a discarding writer as the process default the runner reads, and a single `[migration:up] ... executing:` line ahead of the envelope would turn the run's whole output into something no decoder accepts. Nothing observed the discarded writer before — the posture the installation exists for was unasserted
- the unknown-manager test pins the refusal it always meant to pin: it accepted any error, so a regression that ignored the `--manager` flag and dialed the default database still read as the guard working; it now requires `bunorm.ErrProviderDefinitionNotFound`
- **Behavioural:** the text rendering escapes C0 control characters and DEL visibly (`\n`, `\r`, `\t`, the rest as `\xNN`) in every string it did not write itself — the error text off the wire, the failed statement, the query names, the DATABASE identity block the server answers and the migration names — before the terminal sees them, and before the table cells are measured, so the alignment counts the escaped spelling. A server whose error carried an escape sequence could repaint the operator's terminal or forge lines in a captured log; the failed statement alone keeps its real line breaks, which are the readability of the query block, with every other control byte escaped. The json rendering is untouched — its encoder escapes on its own
- the refusal of a held migration lock names the resource and the remedy, on `db:migrate` and `db:rollback` alike. Bun's own error states that a lock exists and nothing else — not which database it belongs to, and not that this command set ships `<prefix>:unlock` to clear one a crashed process left behind — and it reached the operator carrying no melody context at all, while the unknown-manager refusal three lines away has always named both what was asked for and what exists. The manager label, the lock table and the unlock command travel in the context now, with bun's error kept as the cause so `errors.Is` still reaches it. **Behavioural change** for anything matching on the message text — see the framework's `UPGRADE.md`
- a migration group that fails part way through names the migrations that already landed, on the plain text and in the machine document alike. Bun returns them beside the failure and the command threw them away, so the operator was told which migration broke and nothing about which had been applied and recorded — leaving the choice between re-running (safe) and rolling back (which would take the landed ones with it) impossible to make without reading the database by hand, and `data` was `{}` for a run that had applied two migrations. The last entry of bun's group is the migration that failed rather than one that succeeded — the group is fixed to the slice up to and including the one about to be attempted, before it runs — so it is counted out, which matches the migrations table exactly, since the migrator marks a migration applied only once its `Up` returns. A run that landed nothing reports no applied block at all rather than an empty one claiming a partial state
- `db:migrate` prints a success line on the run that changed the schema. The line lived inside `wantsDetail()`, so a plain run — the shape a deploy script invokes — printed `WARNING: no pending migrations` for the run that did nothing and not one byte for the run that applied five: the log was empty exactly when something had happened, and the operator reading it at three in the morning could not tell the two apart, while the `db:rollback` sibling has always printed its own. The line names the applied count, the manager label the run rode and the group; the name list stays behind `--verbose` and the json document is deliberately unchanged, since under it the same run already carries all three as structured fields
- the json envelope carries the failure's own context under `error.details` and the whole chain under `error.cause.details.chain`. Both were `nil` on every failure alike, so the machine document — the contract a pipeline reads — was the single rendering that threw away what the error already carried: a wrong password answered one sentence beside `"details":null,"cause":null` on stdout while the journal, over the same value at the same instant, filed the connection, the pool sizing, the deadlines and the driver's own refusal. The details object stays an object when the failure carries no context, so the field keeps its json type on every failure
- the document keys the migration blocks apart from the headings a person reads: `data.migrations.applied` answers the same thing from `db:status` and from `db:migrate`, where the heading string used to be the key — `APPLIED` against `APPLIED MIGRATIONS` — leaving the set of keys unenumerable and a rename for readability breaking every consumer silently. `data.database.database` is json `null` for a connection reporting no current database, where as a string it was indistinguishable from a database named literally `<null>`
- a failed lock release fails the command. The failure was printed and then dropped, so a migrate or rollback whose unlock failed exited 0 while the lock row survived — a deploy script read success over a database that refuses every later migration on every replica. **Behavioural**: the exit code now reports it; a migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it
- `RunQueries` and `RunQueriesWithOption` report an empty query set as a warning instead of printing "all 0 queries executed successfully". The run still succeeds — the caller decides what an empty migration means — but under `WithMarkAppliedOnSuccess(true)` the migration is marked applied and never runs again, so a builder that produced nothing must not read like the queries ran
- `RegisterCommands` refuses a nil migration set instead of returning no commands at all. The silence left a direct caller believing it had registered the command family and finding out at invocation time, as "unknown command", far from the wiring that caused it; `Module` gates its own optional set before calling, so a binary registering only migration contexts is unaffected, and a `ModuleConfig` carrying neither `Migrations` nor `Contexts` is refused by name for the same reason. **Behavioural**: `RegisterCommands(nil, ...)` now panics at registration
- table cells are truncated by runes, not bytes: the format widths pad by rune count, so a multi-byte value was truncated even when it fit the column, with the cut landing mid-rune and rendering as a replacement character; a budget too small for the ellipsis no longer slices negative
- README — the flags section no longer claims `--format=json` is unimplemented, and no longer says `RegisterCommands` answers an empty slice for a nil set; the `ManagerRegistryServiceId` option is documented as selecting the registry **instance** rather than an implementation, and the dedicated migration connection is attributed to all six commands, which share one manager resolution

- a migration read from a `.sql` file whose statement the database refuses now fails the command instead of being reported as applied. The module required `bun v1.2.16`, where the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec failure with its own nil return, so `Migrate` answered nil for a statement that never ran: `db:migrate` printed `[success]`, exited 0, wrote nothing to the journal, and marked the migration applied — which made the failure unrepeatable and `db:status` report it as done. The requirement moves to `bun v1.2.17`, the oldest release that returns the exec failure, and a regression pin drives a refused `.up.sql` through the migrator built here and requires both halves: the refusal reaches the caller, and no row is written for it. Go migrations were never affected
- the migration lock is released on a context detached from the command's own, so interrupting a running migration no longer leaves the lock row behind and refusing every later migration until someone runs the unlock command by hand. The cancelled context made the delete fail before it reached the database, and the failure was discarded; it is now reported

## [v3.1.0] - 2026-07-11 - Multi-Context Migrations and Per-Context Manager Pinning

### Added

- `context.go`, `register.go`, `module.go` — multi-context migrations for a binary with several databases: `ModuleConfig` gains `Contexts []ContextConfig{Name, Migrations, Options}`, and one module registration now generates a full per-context command family — `db:<name>:init|migrate|rollback|status|unlock|create` — instead of the module being registered once per database with hand-managed prefixes, separate registries and `container.WithoutTypeRegistration()`. Each context resolves against the one shared `*bunorm.ManagerRegistry` and is pinned to its manager (by convention the manager name equals the context name; the command prefix derives as `<basePrefix>:<name>`), and the plain `RegisterContextCommands(contexts, baseOptions)` form exists for hand-wired setups. The legacy single-set form is untouched and composes with contexts in the same `ModuleConfig`.
- `option.go` — `Options.ManagerName` pins the registry manager a command set uses when the `--manager` flag is absent, replacing the fall-through to the registry default. Resolution order: `--manager` flag, then the pin, then `registry.DefaultManager()`. This closes the multi-context foot-gun where omitting the flag silently migrated whichever context happened to be the shared registry's default.

### Changed

- `context.go` — a migration context's `ManagerName` no longer inherits the base `Options` pin; a context targets the manager named after it (its own database) unless it sets its own `Options.ManagerName`. Reusing one base `Options{ManagerName}` (via `NewModule`'s shared `Options`, say) could otherwise silently migrate every context against that one manager's database. The other fields — `ManagerRegistryServiceId`, `ManagerFlagName`, `CommandPrefix` — still cascade from the base options.

## [v3.0.3] - 2026-06-16

### Added

- `v3/README.md` — added a v3 module README documenting `RegisterCommands`, the `Options` defaults, the `CliModule` wiring, and the generated `db:*` migration commands.
- `v3/module.go` — `migrate.NewModule(ModuleConfig{Migrations, Options})` self-registering application module that registers the migration commands, so `app.RegisterModule(migrate.NewModule(...))` replaces a hand-written `RegisterCommands` call into the application's `RegisterCliCommands`. v3 binding.

### Fixed

- `context.go` — a base-level `Options.ManagerName` pin is inherited by every context, as its own doc and each sibling field already promised. An empty per-context `ManagerName` jumped straight to the context name, so the pin was silently ignored and each context's commands targeted a manager merely named after it.
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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.3...integrations/bunorm/migrate/v3.1.0

[v3.0.3]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.2...integrations/bunorm/migrate/v3.0.3

[v3.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.1...integrations/bunorm/migrate/v3.0.2

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/migrate/v3.0.0...integrations/bunorm/migrate/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/migrate/v3.0.0
