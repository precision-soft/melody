# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/migrate/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- under `--format=json` every command renders one machine-readable document — the blocks as data, the side-reports as warnings, the failure inside the envelope's error — where the flag was declared and validated by `StandardFlags` long before it was honoured and `db:status --format=json | jq` failed on the first byte of a plain-text table.
- `SetDefaultRunnerOption` — the migrate and rollback commands install their parsed writer and colour posture as the process default `RunQueries` reads when a migration passes no option, since a generated migration's signature is fixed by bun as `(ctx, db)`; a `--no-color` run no longer carries escape codes on its per-query lines, and under json the per-query progress is discarded.
- the verbose DATABASE block is answered for PostgreSQL too, through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`, where it was silently absent on a pgsql-backed manager; a unix-socket connection reports its host as `<local socket>`.

### Changed

- **Behavioural change**: `details.group` in the `--format=json` document of `migrate` and `rollback` is the group's bare id, `"1"`, on success as on failure, where a successful run spelled it `"group #1 (...)"` and a failed one `"1"`; the text lines say `group 1`. A reader that parsed the success spelling reads the id directly.
- **Behavioural change**: `db:migrate` and `db:rollback` hand their `--no-color` and `--format` posture to the migrations through the context the migrator passes them, and install it as the process-wide fallback only for the length of the run, restoring what was there: a migration passing its context on to `RunQueries`, as the generated skeleton does, prints under the command that ran it, and one that drops it no longer sees the last command's writer for the life of the process. `SetDefaultRunnerOption` stays the door of a host running migrations on its own.
- **Behavioural change**: the commands no longer pre-print the failure they return, which the cli runner's `[error]` line and the log record already report; an unlock failure beside a failed migration is still printed, since the return keeps the migration's error.
- `db:create` succeeds with a warning naming the file and that its directory entry may not survive a crash when only the fsync of the directory failed after the rename landed, where a failure sent the operator to create a second migration, every failure before the rename staying the verdict; the migration commands run on the dedicated migration connection when the provider offers one (`bunorm.MigrationProvider`), only that connection labelled `(dedicated migration connection)`, falling back to the ordinary pool otherwise, where a long DDL was cut mid-sequence by the request pool's 30s deadlines with `invalid connection`; the json envelope is one line, `db:status --format=json` and its siblings rendering one closed document and `--format=json-pretty` the same indented.

### Fixed

- **Behavioural change**: the text rendering escapes C0 control characters and DEL visibly (`\n`, `\r`, `\t`, the rest as `\xNN`) in every string it did not write — the error off the wire, the failed statement, the query names, the DATABASE block, the migration names — before the terminal sees them and before table cells are measured, so a server error carrying an escape sequence can no longer repaint the terminal or forge log lines; the failed statement keeps its real line breaks, and the json rendering spells the C1 block as `\u00NN` escapes.
- **Behavioural change** for anything matching on the message text: the refusal of a held migration lock, on `db:migrate` and `db:rollback`, carries the manager label, the lock table and the `<prefix>:unlock` command that clears it, with bun's error kept as the cause so `errors.Is` still reaches it — see the framework's `UPGRADE.md`.
- **Behavioural change**: a failed lock release fails the command, where a migrate or rollback whose unlock failed exited 0 over a lock row that refuses every later migration on every replica; a migration that itself failed keeps its own error as the verdict, the unlock failure printed beside it.
- **Behavioural change**: `RegisterCommands(nil, ...)` panics at registration, where `RegisterCommands` answered no commands and the caller found out at invocation as "unknown command"; a `ModuleConfig` carrying neither `Migrations` nor `Contexts` is refused by name, and `Module` gates its own optional set.
- the json document: it carries the warning of a migration that ran no queries under `warnings`, `meta.arguments` names the positional arguments (`db:create` included), the runner's progress is pinned out of it under `--format=json`, where one `[migration:up] ... executing:` line would make the output undecodable, the failure's own context is under `error.details` and its chain under `error.cause.details.chain`, where both were `nil` on every failure, `"details":null,"cause":null`, the details staying an object when the failure carries no context, and `data.migrations.applied` answers alike from `db:status` and `db:migrate`, keyed apart from the headings `APPLIED` and `APPLIED MIGRATIONS`, `data.database.database` being `null` rather than a string `<null>` for no current database.
- every command runs inside one frame that installs the recovery, so a panicking migration is that command's failure and its `Run` no longer writes a success document carrying `"error":null` — the shared door takes `recover()`'s value as a parameter, renders the real failure — an error-shaped panic value in the cause slot with its context and chain, any other value as the value it is — and re-raises the panic unchanged; `db:create`'s lost-report journal is armed inside the frame; `errorDetailsOf` reads its `errors.As` target through the typed-nil door, as `errorCauseOf` does through `BuildCauseChain`, where a typed-nil link bun wraps with `%w` (its `fmt` text `<nil>`) made `Context()` panic on a nil receiver inside the rendering and lost the envelope.
- the migration lock: a refused lock asks the locks table, where bun files every failure of its insert as "already locked" — a row present is held, with the unlock command, a missing or unreadable table and a cancelled context are named as such, and a failure while no lock is held carries no remedy, bun's error staying the cause, on rollback too; a failed release is wrapped to say the lock stays held, in `bun_migration_locks`, and which unlock command clears it, where `--format=json` showed one `migrate.warning` of `"context deadline exceeded"` from `db:migrate` or `db:rollback`, the driver error reachable by `errors.Is`; the release runs on a context detached from the command's, so interrupting a run no longer fails the delete by itself, a failed release still reported.
- the failure report: a migration whose `Up` ran and whose mark in `bun_migrations` failed is named apart, under `ranNotRecorded` and with a warning not to run it again but to record its row or roll its change back by hand, told by bun's `<name>: up: ` prefix on a failed `Up`; a group that fails part way names the migrations that already landed, where `data` was `{}`, the failing one counted out, and a run that landed nothing reports no applied block at all; `db:migrate` prints a success line with the applied count, the manager label and the group, where outside `wantsDetail()` a run that applied five printed nothing beside `WARNING: no pending migrations` for one that did nothing, as `db:rollback` always did, the names behind `--verbose`.
- rollback: a rollback that fails part way names the group it was walking, on the text block and in the machine document, its count travelling under `status`, the slot that already carries `initialized` and `3 pending`, so the text block shows it too: `data.details.status`, where an unreleased build answered `data.details.migrations`.
- the escaping of text the commands did not write: the direction `RunQueries` takes is escaped as the migration name is, the C1 block (`U+0080`–`U+009F`) and the separators `U+2028`/`U+2029` alongside C0 and DEL, a server answering `U+009B` having repainted the terminal and `U+0085`, `U+2028` and `U+2029` forged lines, a rune above one byte spelled `\uNNNN` and C0 and DEL keeping `\xNN`; a byte outside valid UTF-8 renders `\xNN` of itself, where the raw introducer `0x9b` passed and other control characters replaced it with U+FFFD; the warning, success and files lines escape the manager, bun's paths and the migration name, and the `[migration:up] <name> [n/m]` prefix is escaped once per run, the json document spelling the C1 block as `\u00NN`.
- `db:create`: it writes the migration file without opening the database, bun's generator writing the template with `os.WriteFile` offline, and `db:create --manager=<name>` refuses a manager the registry does not know through `HasProviderDefinition`, as for `Options.ManagerName`, with the container's `service is not registered` when no registry is registered, where a misspelt `--manager` wrote the file and surfaced at `db:migrate`; it holds the name to `^[0-9a-z_-]+$`, and the FILES block survives a create that answered no file; it finishes bun's write atomically through a temporary `.melody-migrate-*.tmp` fsynced, renamed and the directory fsynced, where a crash left a truncated Go file under a legitimate name, keeping the mode the destination had (`umask 077` leaves 0600), bun's 0644 the fallback when it cannot be read; its missing-name refusal names the family, `db:archive:create <name>`, not a hardcoded `db:create`; and a text report cut short by a lost write is refused by `finish` on every other command, in text and under the json document alike, the command's own failure still winning, where a full disk exited zero, while `db:create` records it as a warning in the application's journal, the file under `path`, and answers success, under `--format=json` too, where the cut document on stdout is not valid JSON.
- the runner option: the per-query posture travels on the context and `RunQueries` reads it before the process default, where two concurrent commands printed under whichever installed last; an explicit `RunnerOption` without a `Writer` passed to `UpWithOption`, `DownWithOption` or `RunQueriesWithOption` takes the command's writer, then the process fallback, then standard output, not standard output in the middle of a `--format=json` document; overlapping commands, which the cron runner dispatches concurrently, put back the host's `SetDefaultRunnerOption` posture whichever order they finish in, a host value installed while they run kept, and one leaving while others run puts back the posture of a command still running, and the put-back is a compare-and-swap so a host value landing before it stays, where `SetDefaultRunnerOption`'s promise was broken; `RunQueries` and `RunQueriesWithOption` report an empty query set as a warning, the run still succeeding, since `WithMarkAppliedOnSuccess(true)` marks it applied for good.
- every command ends the dedicated migration connection on its way out through the registry's `CloseMigrationDatabase`, where a boot-time migration kept a deadline-less connection for the process's life, the ordinary pool of a provider without one belonging to the application and left untouched, and a failed close is reported as a warning rather than dropped; a refused statement of a `.sql` migration fails the command, the requirement moving from `bun v1.2.16`, whose deferred `conn.Close()` / `tx.Rollback()` overwrote the failure so `Migrate` answered nil, `db:migrate` printed `[success]` and `db:status` reported it done, to `bun v1.2.17`, with a pin driving a refused `.up.sql` that requires the refusal at the caller and no row written; Go migrations were never affected.
- the shared test material moves to `fixture_test.go` out of `migrate_test.go`, and `control_character.go` gets its mirror; the unknown-manager test requires `bunorm.ErrProviderDefinitionNotFound`, where any error passed for a regression ignoring `--manager`; table cells are truncated by runes, not bytes, and a budget too small for the ellipsis no longer slices negative.
- README — the flags section no longer calls `--format=json` unimplemented nor says `RegisterCommands` answers an empty slice for a nil set; `ManagerRegistryServiceId` selects the registry **instance**, and the dedicated migration connection is attributed to all six commands.
- documentation: `db:unlock` says in its description and the README that it clears the lock whatever holds it, so it is run only when no migration runs; the unreleased entries spell the behavioural-change marker in full and the two refining entries no longer carry it; `resolveDatabase` states which frame defers what, and that no test drives a failing release.

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
