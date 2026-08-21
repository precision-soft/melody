# UPGRADE

This document records, per release, every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. Releases are listed newest first.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

Melody releases a behavioural break as a **MINOR**, with the entry marked `**Behavioural change**` in the changelog and listed here with its symptom and remedy. It does not open a new major for one.

The same decision covers a **method added to an exported contract**, which breaks an out-of-tree implementation of that interface at compile time: it ships as a MINOR with a `**Breaking**` note. A new major would put `/v4` into the import path of every file of every consumer — the cost is paid by everyone, including the majority that implements no framework contract — to spare the one consumer that implements it the addition of a single method. That is the same cost already rejected for behavioural breaks, so it is rejected here too.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Unreleased

Every entry below is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. Two of them lost data — both in the `awss3` object storage integration, where a wrongly declared size could replace a stored object with a truncated one and then delete what was left.

This section covers the changes currently sitting in the `[Unreleased]` block of [`CHANGELOG.md`](../CHANGELOG.md); they ship as a MINOR release.

### Security: `NewAccessControlRule` is now segment-bounded, and the cross-segment form has its own name

**What changed.** `NewAccessControlRule` built a rule that matched every path merely beginning with the prefix — `/admin` governed `/administrator` as readily as `/admin/panel`. It now builds a rule bounded to a path SEGMENT: `/admin` governs `/admin` and `/admin/panel` but not `/administrator`. The cross-segment behaviour moves to the explicit `NewAccessControlRawPrefixRule`, on which `PUBLIC_ACCESS` is refused because a raw public rule, being the longest match, shadows a correctly bounded denial. An empty prefix is now refused rather than made a catch-all fallback, and `NewAccessControlRuleWithSegmentPrefix` becomes a deprecated alias for the plain name.

**Symptom.** An existing rule matches fewer requests than before — only its own segment and descendants under a `/` boundary — which can only refuse a request the old raw form would have granted, never grant one it would have refused. A rule that genuinely meant to reach across segment boundaries (a rule for `/admin` that was relied on to also govern `/admin-tools`) stops governing the sibling. A `NewAccessControlRule("")` used as a catch-all fallback now panics at construction.

**Remedy.** For a rule that must reach across the segment boundary, call `NewAccessControlRawPrefixRule`. For a catch-all fallback, declare an explicit `"/"` prefix, or use `NewAccessControlRawPrefixRule("")`. Most rules want the bounded form and need no change.

### Security: a global access control without a firewall now enforces, and a zero-value override inherits it

**What changed.** A `SetGlobal` access control declared without any firewall was silently dropped — `BuildAndCompile` returned nil the moment no firewall was registered — so the global rules enforced nothing. They now enforce: the compiled configuration carries the global control and the listeners fall back to it. Separately, a zero-value `FirewallOverrideConfiguration{}` now inherits the global access control the way `NewFirewallOverrideConfiguration()` does; it previously carried `inheritGlobalAccessControl=false` and compiled an empty control that opened every route behind the firewall.

**Symptom.** A deployment that declared global rules while every firewall sat behind a turned-off feature flag now has those rules apply — requests that used to reach handlers unauthenticated are refused. A firewall built with a bare `FirewallOverrideConfiguration{}` (rather than the constructor) now inherits and applies the global policy instead of leaving its routes open. `WithMergeStrategy` refuses an unrecognised strategy by name where it used to fall back to `localFirst` silently.

**Remedy.** None if the enforcement is what you meant; this closes a fail-open. A firewall that genuinely wants no global inheritance calls `WithInheritGlobalAccessControl(false)` paired with `WithAccessControl`. Correct any merge strategy string the boot now rejects.

### Security: the role voters and `DecideAll` refuse where they used to grant

**What changed.** `RoleVoter` and `RoleHierarchyVoter` granted a token that carried the role even when it answered `IsAuthenticated()` false; they now deny it, matching the access control listener, which had always checked authentication first. `AccessDecisionManager.DecideAll` over an empty attribute list now refuses instead of granting vacuously, matching `DecideAny`.

**Symptom.** A handler-level `IsGranted` check on an unauthenticated token that carries roles — a "remembered" or half-logged-in token — now returns false where it used to return true. A direct `DecideAll(token, nil, subject)` caller now receives a refusal.

**Remedy.** None if you relied on authentication being required, which is the safe reading. A caller that intentionally passed an empty attribute list to `DecideAll` expecting a grant must pass the attributes it means to check.

### Bunorm: the registry refuses new callers while a pool is still closing

**What changed.** `ManagerRegistry.Close` marked the registry closed and then held the registry lock for the whole teardown, closing every manager and every migration database inside the critical section. It now publishes the flag under the lock, takes a snapshot of the two maps, releases the lock, and closes the pools outside it.

**Symptom.** A call to `Manager`, `Database` or `MigrationDatabase` arriving while `Close` is running no longer waits for the teardown to finish; it is refused at once with `ErrManagerRegistryClosed`. Previously such a call parked on the registry lock, and against a peer that had stopped answering — a network partition at shutdown, where the migration connection's write deadlines are deliberately lifted — it could park for as long as the driver waited, so a graceful-shutdown drain expired with goroutines wedged in the registry. Code that relied on that blocking to serialise its last queries behind the teardown now sees the refusal instead.

**Remedy.** None for the ordinary case: the refusal is what the flag has always meant, and every caller already had to handle `ErrManagerRegistryClosed`, which is what the same call answered a moment later anyway. A caller that genuinely needs its work to finish before the pools close must order that itself — run it before `Close`, or gate `Close` behind it — rather than relying on the lock to do the ordering.

### Bunorm mysql and pgsql: a transient marker inside a word is no longer transient

**What changed.** The providers decide whether to retry a failed open by scanning the lowercased error message for a list of markers. The scan matched them as bare substrings, so the short spellings fired inside ordinary identifiers. The markers are now matched as words: a match counts only where the characters on either side are not letters, digits or underscores.

**Symptom.** A permanent failure whose message happens to contain a marker inside a word now fails on the first attempt instead of being retried for the whole budget. The two measured cases are a missing table whose name contains `eof` — `Table 'app.geofences' doesn't exist` — and an unknown column named `session_timeout`; both were retried to exhaustion and then reported as "database connection failed after max retry attempts" rather than as a non-transient failure. Such a boot now fails faster and under the correct classification.

**Remedy.** None is required, and the change is in the safe direction: the failure was permanent in both cases and the retries only delayed the report. The `io.EOF` and `net.Error` checks that run ahead of the message scan are untouched, so a genuine end-of-file or timeout is classified by type as before, and every marker that appears as its own word — `i/o timeout`, `connection refused`, `bad connection`, a bare `EOF` — matches exactly as it did. An operator who wants a permanent failure retried anyway raises the retry budget rather than relying on a substring collision.

### Bunorm mysql: the provider negotiates verified TLS by default

**What changed.** The mysql provider set no TLS on its connector, so it connected in plaintext and offered no option to enable TLS. It now builds a verifying `tls.Config` by default — the system roots, the configured host as the name to verify against, `MinVersion` TLS 1.2 — the same posture its pgsql sibling already carried, and refuses the driver spellings that would downgrade silently.

**Symptom.** A mysql server that speaks no TLS fails the dial where it previously connected in plaintext. The example's development mysql is such a server.

**Remedy.** A database reached over a trusted network, or one that speaks no TLS, arms `mysql.WithInsecure(true)` on the provider — the deliberate opt-out spelled the same way as pgsql's. A database with a certificate needs no change; one needing a pinned or client certificate passes `mysql.WithTlsConfig`. The example arms the opt-out through a new `MYSQL_INSECURE` switch in its `.env`.

### Bunorm: bun's own diagnostics go to the journal

**What changed.** Opening a connection through the mysql or pgsql provider routes bun's package-level logger into the application's journal, once per process, through the new `bunorm.RouteDiagnostics`. Bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule, a query carrying arguments and no placeholders — arrive as warning records under the message `bun diagnostic` with the line in the context.

**Symptom.** Those lines stop appearing on standard error and start appearing in the journal. An operator or a test grepping standard error for `WARN: bun:` finds nothing.

**Remedy.** Read them from the journal, filtering on the `bun diagnostic` message. One line is deliberately unaffected and stays on standard error: the mysql dialect writes `can't discover MySQL version` through the **standard library's** default logger rather than bun's, so routing it would mean taking `log.SetOutput` for the whole process — every dependency and your own `log` calls with it. That is the application's decision; take it in your composition root if you want it, as the mysql readme shows.

### Bunorm pgsql: every driver deadline is named, configured and lifted for migrations

**What changed.** `pgsql.TimeoutConfig` carries `ReadTimeout` and `WriteTimeout` beside `ConnectTimeout`, the connector receives all three (the dial included), and the provider implements `bunorm.MigrationProvider`. Until now the dial ran under pgdriver's internal 5s default whatever `ConnectTimeout` said, every query ran under invisible 10s read / 5s write deadlines, and `db:migrate` ran on the request pool — an 11-second DDL statement died at 10.004s, measured.

**Symptom.** `pgsql.NewTimeoutConfig(connect)` no longer compiles — the constructor takes the three durations, the mysql signature. Behaviourally, the effective read/write deadlines move from 10s/5s to the documented 30s/30s.

**Remedy.** `NewTimeoutConfig(connect, 0, 0)` keeps the connect timeout and takes the 30s/30s defaults; name tighter deadlines where request traffic needs them. Migrations need nothing: `db:migrate` now prefers the dedicated lifted connection automatically.

### Bunorm: the `bun` requirement moves to v1.2.17, dialects and drivers in lockstep

**What changed.** Every module of the `bunorm` family — the manager, `mysql`, `pgsql` and the three `migrate` modules — requires `github.com/uptrace/bun v1.2.17` and, where they carry one, `dialect/mysqldialect`, `dialect/pgdialect` or `driver/pgdriver` at the same version. v1.2.16 swallowed the failure of a migration read from a `.sql` file: the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec error with its own nil return, so `db:migrate` printed `[success]`, exited 0 and marked a migration applied that never ran.

**Symptom.** If your application pins a bun dialect or driver of its own, the build now selects `bun v1.2.17` through this dependency while your dialect stays where it was, and the process **panics at init**: `mysqldialect and Bun must have the same version: v1.2.16 != v1.2.17`. The dialect packages check this themselves; it is not a melody rule.

**Remedy.** Move your own `github.com/uptrace/bun/...` requirements to `v1.2.17` in the same change — `go get github.com/uptrace/bun@v1.2.17 github.com/uptrace/bun/dialect/mysqldialect@v1.2.17` and the equivalent for `pgdialect` / `pgdriver`. Applications that declare no bun dependency of their own need no action.

### Bunorm migrate: the held-lock refusal names the resource and the remedy

**What changed.** `<prefix>:migrate` and `<prefix>:rollback` wrap bun's lock error in a melody error naming the manager label, the lock table and the `<prefix>:unlock` command, with bun's error kept as the cause. It previously travelled as bun's own error, carrying no melody context at all.

**Symptom.** Code matching the refusal on the text `already locked` no longer matches at the top of the chain: `Error()` answers `migrate: the migration lock is held; another migration is running, or a crashed one left it behind`.

**Remedy.** Match through the chain — `errors.Is` and `errors.As` reach bun's error exactly as before — or read the `manager`, `locksTable` and `unlockCommand` keys of the context, which is what the wrap exists to provide.

### Bunorm migrate: a failed lock release fails the command

**What changed.** A `db:migrate` or `db:rollback` whose unlock fails now returns that failure instead of printing it and exiting 0. A migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it. The release also runs on a context detached from the command's own, so an interrupted migration no longer leaves the lock row behind.

**Symptom.** A deploy step that read exit 0 over a surviving lock row — and then found every later migration refused on every replica — now fails at the step that caused it.

**Remedy.** None for a healthy run. For the failure, `<prefix>:unlock` clears a lock a crashed process left behind, and the refusal above names it.

### Bunorm migrate: the json document is not shaped by `--verbose`, and its keys are stable

**What changed.** Under `--format=json`, `db:migrate`, `db:rollback`, `db:status`, `db:init` and `db:unlock` render one machine-readable envelope where they used to print the plain-text blocks, and they collect every block at any verbosity: verbosity remains a rendering decision about the plain text alone, which is what the readme always said. The document keys are stable names rather than display headings — `data.migrations.applied`, `.pending`, `.rolledBack` — and `data.database.database` is json `null` when the connection reports no current database, where the text block renders `<null>`. `--format=json-pretty` is the same document indented for reading by hand.

**Symptom.** `db:status --format=json | jq` used to fail on the first byte of a plain-text table; it now decodes. A json run performs the database-identity query that a text run performs only under `--verbose`.

**Remedy.** Read `data.migrations.applied`, `data.migrations.pending` and `data.migrations.rolledBack`; test `data.database.database` for null rather than for the string `"<null>"`. Anything that parsed the plain text under `--format=json` must decode the document instead — which is what the flag always promised.

### Bunorm migrate: a nil migration set and an empty module configuration are refused

**What changed.** `RegisterCommands(nil, ...)` panics at registration instead of answering no commands, and `NewModule(ModuleConfig{})` — neither `Migrations` nor `Contexts` — is refused by name when the kernel asks it for its commands. Both used to be silent.

**Symptom.** A binary whose wiring passed a nil set, or registered the module with an empty configuration, fails at boot with a named refusal where it used to boot and answer `unknown command` at the first `db:migrate`.

**Remedy.** Pass the migration set, or the contexts, that the registration was meant to carry. A binary that registers only `Contexts` is unaffected: the module gates its own optional set before calling `RegisterCommands`.

### Bunorm migrate: the plain text escapes control characters, and the commands stop pre-printing their failure

**What changed.** Every string the commands did not write themselves — the error text off the wire, the failed statement, the query names, the identity block the server answers and the migration names — is escaped visibly (`\n`, `\r`, `\t`, the rest as `\xNN`) before the terminal sees it, and before the table cells are measured, so the alignment counts the escaped spelling. The failed statement alone keeps its real line breaks. Separately, the commands no longer pre-print the failure they return: the cli runner's `[error]` line and the log record already report it. The json rendering is untouched — its encoder escapes on its own.

**Symptom.** A test asserting an exact rendered line that contained a raw control byte sees its escaped spelling. A console that showed the same failure three times shows it twice.

**Remedy.** Assert on the escaped spelling, which is what an operator's terminal now receives. The deliberate exception stays: an unlock failure beside a failed migration is still printed, because the return keeps the migration's error and would otherwise lose it.

### Bunorm migrate: the verbose DATABASE block answers for PostgreSQL, and cells are cut by runes

**What changed.** The `--verbose` DATABASE block was MySQL-only, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable; it is answered now through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. Table cells are truncated by rune count rather than by bytes, matching the format widths that pad by runes.

**Symptom.** `db:migrate --verbose` against postgres prints a DATABASE block where it printed none. Over a unix socket — the connection a local migration is most likely to take — the host reads `<local socket>`, because `inet_server_addr()` is NULL there. A multi-byte value that fit its column is no longer truncated, and no cut lands mid-rune.

**Remedy.** None. Read the block; it reports the same five fields on both dialects.

### Rueidis: the boot ping is bounded even when the connect timeout is left at zero

**What changed.** `Provider.Open` runs its boot ping under the connect timeout resolved through the same rule the rest of this package reads its zeros by: a non-positive `TimeoutConfig.ConnectTimeout` takes the default (3s) rather than meaning "no bound". It previously built the ping context only for a positive value and ran the ping on `context.Background()` otherwise — with no deadline at all.

**Symptom.** A `TimeoutConfig` naming only the command timeout, the ordinary shape of a partial literal, put the boot ping on an unbounded context. A store that accepted the connection and then stopped answering hung the boot forever, holding a client no one could close yet. That boot now fails after the default bound with `redis connection failed`.

**Remedy.** None for a wiring that meant the ping to be bounded — this is the bound it always documented. A wiring that genuinely wanted an unbounded ping must now say so by turning the ping off (`ClientConfig.PingOnStart = false`) and pinging itself; a value large enough to stand in for "unbounded" also works, and reads as the deadline it is.

### Rueidis cache: `Backend.Close` ends the backend, and `ClearByPrefix` refuses the empty prefix

**What changed.** Two refusals the in-memory backend behind the same contract already gave. `Backend.Close` marks the backend closed and every later operation answers `cache backend is closed`; a handle minted by `BackendService.WithContext` reads its owner's flag, so the service's `Close` reaches the per-request handles the runtime door mints. And `ClearByPrefix("")` is refused as an empty key instead of being read as the whole namespace. The shared `rueidis.Client` is still not closed by the backend: it belongs to whoever built it.

**Symptom.** Code that used a backend after closing it — a teardown-ordering bug — used to keep serving through the client and now fails immediately with a named refusal. Code that passed an empty prefix to `ClearByPrefix`, deliberately or because a prefix assembled at run time came out empty, used to wipe every entry under the backend's own prefix and now gets `cache key is empty`.

**Remedy.** Fix the teardown order so nothing reads through a closed backend, which is what the refusal is for. Where the whole namespace really is meant, call `Clear`, which says so. `WithCommandTimeout` is available for the other half of the same problem: it bounds the operations that carry no caller context, which is the whole ctx-less half of `cache/contract.Backend`.

### Rueidis: a rate limiter store failure is recorded when no observer is wired

**What changed.** With no `WithRateLimiterOnError` observer, a store failure is now recorded by the limiter itself — at error for an outage, at warning for the caller's own cancellation — and the error is marked already-logged so the http sites do not file it a second time. An observer given by the application still replaces that record rather than adding to it, and receives the error untouched and unmarked. The limiter also names the caller's cancellation apart from a store failure, and re-arms the window on a key that reached the store carrying no expiry.

**Symptom.** New records appear in the journal from applications that wire no observer, where a redis outage previously refused every call and reported nowhere at all: `Allow` answers a bool and `Reset` answers nothing, so there was no channel for it to reach. A counter on a key that had lost its ttl — a `PERSIST`, an eviction, a hand-written key — used to climb past the limit and stay there, refusing every request keyed on it permanently under the fail-closed default; it now lapses.

**Remedy.** None required. An application that wants the failures on its own channel and nowhere else wires `WithRateLimiterOnError`, which is what it was always for.

### Rueidis cache: a negative ttl is refused, and a batch failure names one key deterministically

**What changed.** `Set` and `SetMultiple` refuse a negative ttl instead of falling into the branch that writes no expiry at all, and `SetMultiple` judges the ttl before its empty-batch early return. A batch that fails part-way collects every failing key and names the sorted first, with the failed and requested counts beside it, rather than returning whichever failure the map iteration reached first. `Clear` and `ClearByPrefix` scan every node of the client rather than the one connection a single command reaches.

**Symptom.** A negative ttl used to store the entry **forever** — the one value the caller meant to be short-lived was the one value stored without end. A batch that failed named a different key on every call for the same wrong batch, and hid that the entries after it failed too. Against a redis cluster, a wipe deleted one node's share of the matching keys and reported success.

**Remedy.** Zero still means no expiry, as both backends document, so only a genuinely negative duration is affected — usually a subtraction that went past the present. Anything that parsed the batch failure message should read `key`, `failedKeyCount` and `requestedCount` from the error context instead.

### Cron: a module with runner commands and no configuration refuses the boot

**What changed.** `cron.NewModule`'s `RegisterCliCommands` panics when `RunnerCommands` are supplied without a `Configuration`/`ConfigurationFactory`, and when a factory returns nil. Until now the module silently registered nothing and the wiring error surfaced as "unknown command" at invocation.

**Symptom.** A wiring that carried runner commands but never set the configuration now fails at boot naming the missing configuration.

**Remedy.** Set `Configuration` (or a factory that returns one); a parameters-only module — no runner commands, no generator — keeps working without one.

### Cron: an entry routed to another crontab file refuses the in-process runner

**What changed.** `EntryConfig.DestinationFile` joins `Command` and `Instances` in `NewRunnerCommand`'s construction refusal: an entry routed to another crontab addresses an external scheduler, and accepted by the runner as well it executed twice whenever the generated manifests were live.

**Symptom.** A boot that used to succeed panics with `cron: the in-process runner supports only name-scheduled single-instance entries; the entry routes to another crontab file`.

**Remedy.** Keep the routed entry out of the runner's `Configuration` (schedule it only for the generator), or drop its `DestinationFile` if in-process execution is the intent.

### Cron: a clean shutdown is not a job failure

**What changed.** A run the runner's own shutdown cancelled is recorded at warning as `cron: scheduled command cancelled by shutdown` and excluded from the failure aggregate; the runner's failure and abandon records carry the run's `cronRunId`.

**Symptom.** `melody:cron:run --once` interrupted by SIGTERM exits 0 with a warning instead of non-zero with an error record; alerting keyed on `cron runner command failed` stops firing on deploys.

**Remedy.** Key deploy-time alerting on the new warning if the old signal was load-bearing; genuine failures keep the error record, now attributable by `cronRunId`.

### Cron: `Configuration` hands out copies

**What changed.** `Schedule` copies the entry configuration it is handed, and `Entries` copies all the way down — the list, each `*ScheduledCommand` and each `*EntryConfig` behind it, schedule included.

**Symptom.** Code that reconfigured a registration by writing through its own struct after `Schedule(...)`, or through what `Entries` returned — `configuration.Entries()[0].Config.Schedule.Hour = "23"` — no longer changes anything.

**Remedy.** Register the entry with the configuration it should have. `Entries` is an inspector; the registry is written through `Schedule`.

### Cron: the runner writes the machine document, and a job's output goes to the journal

**What changed.** `melody:cron:run` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity` for every command, which used to kill the runner at parse — and under `--format=json` renders one envelope per dispatched minute (`--once` writes exactly one and exits). A scheduled command's own output no longer reaches the process stdout: it is captured and filed as one record per run that printed anything, naming the command and the run id, capped at 64 KiB.

**Symptom.** `melody:cron:run --once --format=json` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". Anything tailing the stdout of `melody:cron:run` for a job's own printed output finds it in the log instead, under `cron: scheduled command output`.

**Remedy.** Read the document rather than inferring the outcome from an empty stream. For a job's own output, read the journal; a job that must write to a stream of its own should open it itself rather than relying on the command writer.

### Cron: a recovered panic carries its cause

**What changed.** The cron runner's recovery boundary hands the panic value on as the CAUSE of the error it fabricates, and captures the stack of the goroutine that raised it. The `panicValue` context key it already wrote is unchanged; `panicStack` is added beside it.

**Symptom.** `errors.Is` and `errors.As` on the run's error now reach the failure underneath, where before they stopped at the fabricated wrapper. Code that relied on those calls answering false for a panicked job will now see them answer true.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked job the same way; the message still says the boundary was a panic.

### Cron: the deprecated abbreviated validation aliases are removed

**What changed.** `ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars` are gone from the cron binding. They were deprecated aliases of `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters`, which are unchanged.

**Symptom.** Code naming an alias stops compiling with an undefined-identifier error.

**Remedy.** Spell the name out; the replacement is a rename, signature-identical. The templates have read `CrontabForbiddenCharacters` since the aliases were deprecated, so nothing behavioural changes.

### Cron: the generated k8s manifests open with the ownership marker

**What changed.** Every file the builtin `k8s` template renders starts with three comment lines carrying `# owned by melody:cron:generate`, the same marker the crontab dialects carry in their header block, and the template renders the marker header alone — demanding no container image — when it has no entries. That is what lets `--prune` reconcile a k8s output directory: the sweep empties only a file whose first bytes prove this generator wrote it.

**Symptom.** Manifests gain three leading YAML comment lines; a byte-exact comparison against previously generated files sees the difference. `kubectl apply` reads comments as comments — the documents are unchanged.

**Remedy.** Regenerate; anything diffing manifests byte-for-byte should regenerate its baseline. Nothing to do for the cluster.

### Cron: a malformed heartbeat opt-in fails the generation

**What changed.** A `melody.cron.heartbeat.enabled` value that does not hold a boolean fails `melody:cron:generate` with an error naming the parameter, under every template. It used to be read as "not enabled".

**Symptom.** A generation that silently produced a crontab without the liveness line the operator asked for — a misspelling indistinguishable from never having asked — now exits non-zero naming `melody.cron.heartbeat.enabled`.

**Remedy.** Fix the value; `true`/`false` in any spelling the parameter conversion accepts. Removing the parameter keeps the opt-in unset, which stays legal.

### Cron: a relative parameter path anchors at the project directory

**What changed.** A relative path read from a parameter — `melody.cron.destination_file`, `melody.cron.logs_dir`, `melody.cron.heartbeat_path` — is anchored at the project directory, the way melody resolves `kernel.logs_dir` and its siblings. A relative path typed as a cli flag stays relative to the working directory, as a shell path should.

**Symptom.** `melody.cron.logs_dir = var/log/cron` used to mean a different directory depending on where the binary was invoked from — under a supervisor starting from `/`, the generated crontab baked `/var/log/cron` into itself. The shipped defaults carry `%kernel.project_dir%` and never moved.

**Remedy.** None for parameters using the shipped `%kernel.project_dir%`-anchored defaults or absolute paths. A deployment that relied on a relative parameter path following the working directory should either make the path absolute or run the generator from the directory it means.

### Cron: the generator writes the machine document and names what a failed run left on disk

**What changed.** `melody:cron:generate` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity`, which used to kill the generator at parse — and under `--format=json` renders one closed envelope: `data.writes` and `data.pruned` as lists on every run, the failure inside the envelope's error with its context and cause chain. In text mode a failed run prints the writes and sweeps that had already happened before returning the failure.

**Symptom.** `melody:cron:generate --format=json | jq` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". The heartbeat-only text line now says `wrote heartbeat-only crontab to …`, as the frozen majors' does.

**Remedy.** Read the document rather than inferring the outcome from the exit code alone; anything matching the old `heartbeat-only file` text updates the one word.

### CLI: `--format=json` writes one document per line

**What changed.** The json printer no longer indents. Every melody command's `--format=json` envelope — the framework's `debug:*` family and the core commands it contributes — is now one compact line terminated by a newline, where it used to be a block of indented lines. `--format=json-pretty` is the same document with the indentation back.

**Symptom.** Output that was read by eye, or a test asserting the rendered text with the spacing `encoding/json` puts after a colon (`"error": null`), sees the compact spelling instead (`"error":null`). Nothing that decodes the document is affected: it is the same document.

**Remedy.** For reading by hand, use `--format=json-pretty`, or pipe through `| jq`, which the documentation already recommended. For an assertion on rendered output, decode the document and assert the value rather than the text — the format the printer chooses is not part of what the command reports. Consumers that read the stream a document at a time, and every `jq` pipeline, need no change at all; the reason for the change is the consumers that could not work before, since a long-running command that renders a document per unit of work promised a stream of closed documents and emitted fragments.

### Cli: a duplicated flag name and a mismatched table row fail fast

**What changed.** `output.MergeFlags` panics on a flag name declared twice, and on a nil flag — the parser resolves a name to the first declaration, so a command-specific flag reusing a standard name was silently inert. `TableBlockBuilder.AddRow` panics on a row whose cell count disagrees with the block's declared columns — a surplus cell silently never rendered; the single-token separator row stays admitted.

**Symptom.** A command whose flags redeclare a standard name, or whose table rows disagree with their block's columns, panics at registration or at the row instead of silently misbehaving.

**Remedy.** Rename the colliding flag (the standard names are the `FlagName*` constants), or make the row's cell count match the columns.

### Cli: negative values for the standard integer flags are refused

**What changed.** `--verbosity`, `--limit`, `--offset` and `--table-width` carry validators refusing a negative value, the way `--format` and `--order` refuse an unsupported one. A negative was clamped to zero, and zero means unlimited for the limit — an argument asking for less than nothing silently delivered everything.

**Symptom.** `--limit=-5` fails at argument parsing, naming the flag, instead of listing everything with exit 0.

**Remedy.** Pass a non-negative value; `0` keeps meaning unlimited/default.

### Cli: the table format stops hiding warnings and errors, and printing failures fail the command

**What changed.** Three output changes in the table format. The `WARNINGS` block renders under `--quiet` too — with `StandardFlags` defaulting quiet to true, an application command's warning was invisible at every verbosity; the warning details stay behind `--verbose`. The envelope error now renders whole (message, code, details, cause) — it previously rendered nowhere in the table format. And the first write failure is returned instead of discarded, so a report truncated by a full disk no longer ends in a success banner and exit zero.

**Symptom.** Quiet table runs may print new `WARNINGS:`/`ERROR:` lines; a run whose output stream fails now exits non-zero.

**Remedy.** None for correct runs; output parsers that assumed quiet suppressed warnings read the json format instead, which has always carried both.

### Cli: a failed run reaches the application log

**What changed.** The exit-coded errors built from a rendered envelope and from the command-suggestion refusal travel unmarked, so the exit path logs them through the application logger before the teardown. They were pre-marked as logged while the rendered report lived only on stdout/stderr — a failed run was invisible to anything reading the log file.

**Symptom.** The log file gains one record per failed command run and per mistyped command name. Exit codes are unchanged.

**Remedy.** None; log-volume alerts keyed on error records may need the new entries accounted for.

### Cli: the command action leaves the container to the process-exit owner

**What changed.** A registered command's action closes the request scope and reports its teardown failure beside the command's own error, and no longer closes the service container. The handler that owns the process exit closes it, after it resolved the logger the final record is written through.

**Symptom.** A container close failure is now reported by the exit handler rather than folded into the command's aggregate, and a failed command's final record is written through the live application logger instead of the stderr fallback. A command that closes the container itself is unaffected.

**Remedy.** None. A command that relied on the container being closed by the time its action returned should close what it owns itself, or use the scope.

### Application: a teardown failure on Run's normal return exits non-zero

**What changed.** When `Run` returns without a panic, the teardown it performs turns a failure it discovered itself — a container close that errored, a flush that failed — into exit code 1. Previously the failure was logged at emergency and the process exited 0.

**Symptom.** A supervisor that restarts on non-zero now sees a shutdown that lost something, where it used to record a clean stop whose only trace was one log line. A close somebody else already performed keeps reporting through its own channel and its own exit code, so nothing is reported twice.

**Remedy.** None for a healthy teardown — exit 0 is unchanged. A deployment that alerted on the emergency record alone can keep doing so; the exit code is an additional signal, not a replacement.

### Application: a teardown that hangs is abandoned and exits non-zero

**What changed.** The normal return of `Run` closes the container through the same ten-second shield the panic path now uses, and takes exit code 1 when the budget runs out. Previously the clean path had no budget at all: one `Close` that never returned parked every service behind it and the process with them, so the healthy shutdown was the one without an emergency exit while the dying one had a way out.

**Symptom.** A process whose teardown blocks for more than ten seconds now prints one line to stderr naming the abandoned step and exits 1, where it used to hang until the supervisor killed it.

**Remedy.** None required — the exit is the intended outcome. A service whose `Close` legitimately takes longer than ten seconds should bound its own work: the shield abandons the step, it does not shorten it.

### Logging: every fatal exit writes a certificate record at emergency level

**What changed.** The process-boundary exit handler writes one record at emergency level — "process exiting after unrecovered error", with the exit code and the error in its context — through whatever logger it resolved, always. The detailed record of the failure is still written at the error's own level, which an operator threshold can silently discard; the certificate is the one record no threshold drops.

**Symptom.** A log pipeline sees one new emergency record per fatal exit, beside the detailed record it may or may not have been keeping. A pipeline alerting on emergency level now fires on every fatal exit, which is what the level exists for.

**Remedy.** None for most deployments — the record is the signal that was missing. A pipeline that must not see it filters on the record's message, which is stable.

### Logging: the correlation id wins the context key

**What changed.** The correlation decorator — `NewRequestLogger` on the request path, the new `NewProcessLogger` on the console path — writes the real id under the context key unconditionally. A non-empty string already sitting under the key used to make the decorator keep it and drop the real id. On the request path a different non-empty string claim now survives under the key suffixed `Claimed`; on the console path the caller's value survives verbatim, whatever its type, under the key suffixed `Provided`.

**Symptom.** A record whose context carried its own `requestId`/`processId` value now shows the generated id under that key and the caller's value under the suffixed one. Anything grepping the log by the id melody generated now finds every record of the run, including the ones that used to escape the chain.

**Remedy.** A consumer that deliberately overrode the key should read its value back from the suffixed key; everything else needs no change — the correlation is simply no longer forgeable from a context value.

### Config: a late secret marking travels to the parameters assembled from the key

**What changed.** `MarkSecret` called after the boot resolution now propagates to every parameter whose template reads the marked key, and follows the marking to a fixpoint through derivation chains — exactly what a marking arriving before the resolution has always done. The scan reads the raw templates; a match inside doubled-percent escaped text over-marks, which errs toward redacting more, never less.

**Symptom.** `debug:parameters` redacts values it used to print: the dsn assembled from a password marked late now shows as secret beside the key itself. No stored value changes — the marking governs display, not storage.

**Remedy.** None. A parameter that must stay visible should not read a marked credential in its template.

### Httpclient: a basic credential travels whenever it was asked for

**What changed.** `WithBasicAuth("", password)` now sends the credential. The username guard used to drop the whole authorization silently, so an api key spelled as the password of an empty user — the shape of curl's `-u :key`, legal under RFC 7617 — produced an unauthenticated request presented as a success. A typed-nil authorization or basic half now leaves the header unwritten instead of panicking on the request path.

**Symptom.** A request built with an empty username and a non-empty password reaches the server with an `Authorization` header it never carried before.

**Remedy.** None for a correct caller. A caller that relied on the empty username to mean "no credential" passes no basic authorization at all.

### Httpclient: the response body cap binds the stream, the default included

**What changed.** `RequestStream` enforces `MaxResponseBodyBytes` — the cap was completely inert on the streaming path — and the inherited default of ten mebibytes now binds a stream whose caller never named a cap. Reading past the cap answers "response body exceeded max size" with the cap, the method and the sanitized url in the record. An invalid cap is also refused before anything is dialled, on both paths, so a POST no longer commits its side effect before being told its options were invalid.

**Symptom.** A stream that delivers more than ten mebibytes — a large download, a long-lived server-sent-event feed — errors mid-read where it used to run unbounded.

**Remedy.** Name the cap the stream actually needs through `WithMaxResponseBodyBytes`; a genuinely unbounded consumer sets one sized far above its expected traffic. Callers of the buffered `Request` are unaffected — the default has always bounded them.

### Opaque tokens: a stored token with no issue instant is refused once its user is revoked

**What changed.** A revocation is no longer an enumeration. [`security/contract.RevocationEpochStore`](../security/contract/token_store.go) publishes a boundary per user, and per device of a user, and [`Lookup`](../security/in_memory_token_store.go) refuses a token issued before the boundary that governs it. This closes the window [`DeleteByUser`](../../integrations/rueidis/v3/token_store.go) could never close: it walks an index with `SSCAN`, which does not promise to return a member added while the walk is in progress, so a token issued during a revocation survived it. The comparison needs an issue instant, so [`security/contract.Claims`](../security/contract/token_validator.go) carries `IssuedAt`, stamped by the store on every write.

Nothing breaks at compile time. The new methods live on their own interface, composed into `EpochRevocableTokenStore` rather than added to `RevocableTokenStore`, so an out-of-tree token store still satisfies the interface it was written against — it simply cannot publish boundaries, and a caller that needs one is told so by `EpochRevocableTokenStoreMustFromResolver` at the moment the service is asked for.

**Symptom.** A token stored by an earlier release carries no issue instant. The zero instant precedes every boundary, so the first time `RevokeBefore` is called for a user, that user's pre-upgrade tokens stop resolving — including ones an operator did not mean to end. Users nobody revokes are unaffected: with no boundary there is nothing to compare against and the token resolves exactly as before.

**Remedy.** None is needed in the ordinary case, and the behaviour is the safe direction: the tokens that stop resolving belong to an account somebody deliberately revoked. If an upgrade must not end any pre-upgrade session, do not call `RevokeBefore` until the longest token lifetime has passed since the deploy; every token written after it carries an instant and is compared normally.

Two consequences worth knowing before wiring it up. A token whose issue was in flight across a revocation is refused — the instant is stamped before the write reaches the store, so a token stamped just before the boundary and written just after it is treated as predating it. That is over-strict rather than under-strict, and deliberate. And the instants come from application clocks, so a node whose clock runs ahead of the node a revocation is issued from stamps tokens that read as later than the boundary and survive it: the window is exactly the skew between the two, and a single node whose clock steps backwards — an NTP correction, a restored snapshot, a resumed virtual machine — produces the same thing without any second node. `WithTokenStoreMaximumClockSkew` on the redis store, and `JwtConfig.RevocationEpochSkew` on the json web token path, bound that window: they widen the boundary by the stated amount and, on the store, additionally refuse a stamp further ahead of the verifying
node than the same amount. Both default to zero, which leaves the behaviour of this release unchanged; set them to the worst skew the fleet can carry. The cost is symmetrical and deliberate: a token issued within that window AFTER a revocation is refused too. `WithRevocationEpochRetention` is unrelated to any of this — it floors how long a boundary is kept when there is no index deadline to adopt, and does not affect the comparison.

### Messagebus: an unhandled consumed message fails the dispatch

**What changed.** A message whose type has no registered handler used to pass through the handle middleware with a warning and nil error; the consume command then Acked it. The default now refuses the dispatch, and the opt-in is `HandleOptions.AllowMissingHandler`, which replaces `RequireHandler` (the same switch, inverted, so the zero value is the safe cell).

**Symptom.** On the consume path, a forgotten `RegisterHandler` line — or a handler registered for `T` while the transport decodes `*T` — used to drain the queue one warning at a time: every message Acked and destroyed, the retry, dead-letter and failure-transport machinery never engaging because the pipeline was told the message was handled. The same mistake now nacks into exactly that machinery and is impossible to miss.

**Remedy.** Code that set `RequireHandler: true` deletes the field — that is the default now. A bus that genuinely wants pass-through (a tap that observes some types and forwards the rest) sets `AllowMissingHandler: true` and keeps the old behaviour, warning included.

### Mailer: configured smtp credentials fail closed when the server does not advertise AUTH

**What changed.** With a username configured, a server whose EHLO response does not advertise `AUTH` is refused — whatever `RequireAuth` says. `RequireAuth` keeps its other half: it still refuses the configuration in which authentication is required but no username is set.

**Symptom.** The old default skipped the whole auth branch and delivered the message as anonymous submission while reporting success, so the configured identity went quietly unused — most commonly against a relay that only advertises `AUTH` after `STARTTLS`, on a session where tls was not negotiated. Deployments in that shape now get an error naming the unapplied credentials instead of silent unauthenticated delivery.

**Remedy.** For a relay that genuinely takes unauthenticated submission, remove the credentials from the configuration — they were doing nothing. For a relay that advertises `AUTH` only after `STARTTLS`, set `RequireTls` so the session upgrade is guaranteed before the auth branch is reached.

### Translation: an absent parameter stays visible, and a misnamed catalog file refuses the load

**What changed.** A plain placeholder whose parameter is absent renders as the visible placeholder itself (`Hello, {name}!`) instead of as an empty string; a parameter present with an explicit nil still renders empty. And `JsonDirectoryLoader.Load` answers a hard error naming any `.json` file that does not parse as `<domain>.<locale>.json`, instead of skipping it.

**Symptom.** Rendered empty, a renamed parameter key shipped every message quietly missing its amount, name or count, with nothing anywhere to learn it from; a golden test that asserted the empty rendering sees the placeholder now. A translations directory in the natural-but-unsupported `en.json` layout used to load zero catalogs successfully, with the runtime symptom — raw message ids in production — pointing nowhere near the mis-named files; that directory now fails the boot with the file named.

**Remedy.** Fix the parameter key, or pass the parameter with an explicit nil where the empty rendering was genuinely wanted. Rename catalog files to `<domain>.<locale>.json` — `messages.en.json` for the default domain — and delete stray `.json` files from the translations directory.

### Compile-level: `messagebus/contract.Transport`'s `Close` lost its runtime parameter

**What changed.** The contract method is `Close() error`; the former `Close(runtimeInstance runtimecontract.Runtime) error` is gone, and `RegisterTransports` now registers a `TransportsCloser` the container's ordered teardown reaches.

**Symptom.** A userland transport fails to compile against the interface until the parameter is deleted. The old signature was structurally dead: the teardown recognizes `Close() error` and nothing else, nothing in the framework or any production wiring ever called a transport's `Close`, so every broker connection lived exactly as long as the process and every deploy tore it down abruptly.

**Remedy.** Delete the parameter from the implementation. A transport that used the runtime for a deadline owns its bound now — the builtin amqp transport already carried its own join timeout and ignored the runtime entirely, which is what made the removal free.

### Compile-level: `container/contract.ScopeManager` and `container/contract.Scope` gained `RegisterScoped`

**What changed.** A scope is now a registrar of its own. [`container/contract.ScopeManager`](../container/contract/scope.go) declares `RegisterScoped(serviceName string, provider any, options ...RegisterOption) error` and `MustRegisterScoped(...)`, which declare a service whose lifetime is one scope — one http request, one command run — built lazily on the first resolution through a scope and closed when that scope closes. [`container/contract.Scope`](../container/contract/scope.go) declares the same two verbs through [`ScopedRegistrar`](../container/contract/scoped_registrar.go), for adding a service to one live scope.

The declaration sits on `ScopeManager` rather than beside the container's own registrations because a scope does not exist until a request arrives: what a scope will own has to be declared at boot by whatever will be creating the scopes.

**Symptom.** An out-of-tree implementation of `container/contract.Scope`, of `container/contract.ScopeManager`, or of `container/contract.Container` — which embeds `ScopeManager` — no longer satisfies the interface, so the assignment fails to compile with `missing method RegisterScoped` or `missing method MustRegisterScoped`. In practice the implementations that break are test doubles: the framework's own sweep had to repair twelve of them, and none of them was production code.

**Remedy.** A double that only stands in for a scope can answer that it registers nothing, which is truthful for a stub and keeps the compiler satisfied:

```go
func (instance *TestScope) RegisterScoped(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
) error {
	return exception.NewError(
		"this scope holds no registrations of its own",
		map[string]any{"serviceName": serviceName},
		nil,
	)
}

func (instance *TestScope) MustRegisterScoped(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
) {
	exception.Panic(exception.FromError(instance.RegisterScoped(serviceName, provider, options...)))
}
```

A double built by embedding `containercontract.Scope` or `containercontract.Container` in a struct keeps compiling untouched and needs nothing — but it will panic on a nil embed if anything calls the new methods, so give it the two methods above if the code under test can reach them.

An implementation that means to carry real scoped registrations should hold the providers apart from the instances it already keeps, build one instance per scope on first resolution, and close what it built when the scope closes. The framework's own implementation is the reference; see [`package/CONTAINER.md`](./package/CONTAINER.md) for what the two lifetimes may read from each other.

See [Versioning policy for breaking changes](#versioning-policy-for-breaking-changes) for why an added contract method ships as a MINOR.

### Compile-level: `session/contract.Manager` gained `RegenerateSession`

**What changed.** [`session/contract.Manager`](../session/contract/manager.go) declares `RegenerateSession(session Session) (Session, error)`, the session-fixation defence: it mints a fresh id, carries the values over, removes the entry the previous id pointed at, and latches the session passed in out of use. The framework's own [`session.Manager`](../session/manager.go) implements it, and [`http.RegenerateRequestSession`](../http/session.go) rotates and republishes in one call.

**Symptom.** An out-of-tree implementation of `session/contract.Manager` — a Redis-backed or database-backed session manager, say — no longer satisfies the interface, so the assignment that hands it to the container fails to compile with `missing method RegenerateSession`.

**Remedy.** Implement the method. It has to mint an id the storage does not already hold, carry the values over, delete the previous entry and put the session it was given out of use, so that a caller who forgets to republish the rotated session is logged out cleanly instead of being left presenting a deleted id:

```go
type CustomSessionManager struct {
	/* the embed stands for the rest of the implementation this excerpt does not repeat */
	sessioncontract.Manager
}

func (instance *CustomSessionManager) RegenerateSession(
	sessionInstance sessioncontract.Session,
) (sessioncontract.Session, error) {
	rotatedSession := instance.NewSession()

	for key, value := range sessionInstance.All() {
		rotatedSession.Set(key, value)
	}

	deleteErr := instance.DeleteSession(sessionInstance.Id())
	if nil != deleteErr {
		return nil, deleteErr
	}

	sessionInstance.Clear()

	return rotatedSession, nil
}
```

The framework's own `Session` is latched out of use rather than merely cleared, because `Session.Set` lifts the cleared flag and a caller that rotated and then kept writing to the original object would otherwise have the response path re-create the just-deleted id and re-issue it as the cookie. That latch is unexported and no contract method was added for it, so an out-of-tree `Session` implementation is only `Clear()`ed — which a later write still undoes. An application that supplies its own `Session` must therefore not write to the object it rotated away.

See [Versioning policy for breaking changes](#versioning-policy-for-breaking-changes) for why an added contract method ships as a MINOR, and [`package/SESSION.md`](./package/SESSION.md) for what a rotation has to guarantee.

### Compile-level: `session/contract.Session` gained `SetShared`

**What changed.** [`session/contract.Session`](../session/contract/session.go) declares `SetShared(key string, value any)`. `Set` stores a value the storage layer is free to copy — every entry is deep-copied on the way into the store and on the way out of it, which is what keeps a session read on one request from writing into another request's data — while `SetShared` stores the handle itself, so `Get` hands back that very value and every reader of the session reaches the one object. The write decides the semantics and `Get` honours whichever was chosen, so there is no `GetShared` and no `IsShared` to implement alongside it.

**Symptom.** An out-of-tree implementation of `session/contract.Session` no longer satisfies the interface, so the assignment that hands it to the framework fails to compile with `missing method SetShared`.

**Remedy.** Implement it. An implementation backed by an in-process map already stores what it is given, so delegating is both the shortest implementation and the honest one:

```go
func (instance *CustomSession) SetShared(key string, value any) {
	instance.Set(key, value)
}
```

An implementation whose storage serialises cannot carry a handle across a round trip, and should refuse the save rather than write something that loads back as a plain copy — [`FileStorage.Save`](../session/file_storage.go) refuses the whole session before touching anything and names the offending key. See [`package/SESSION.md`](./package/SESSION.md) for the distinction and what each write is for.

### Compile-level: `config/contract.HttpConfiguration` gained `StaticExcludedPaths`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares `StaticExcludedPaths() []string`, the path prefixes the built-in file server declines before it looks at the disk. The framework's own implementation reads them from `MELODY_STATIC_EXCLUDED_PATHS` (`kernel.static.excluded_paths`), a comma-separated list that is empty by default. Since the built-in file server sits outermost in the pipeline, excluding a prefix is how an application takes a part of the url back — to put authentication in front of a directory, or to serve it from a root of its own.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` — a test double, or a configuration assembled in code rather than from `.env` artifacts — no longer satisfies the interface, and the assignment fails to compile with `missing method StaticExcludedPaths`.

**Remedy.** Implement it. An empty list excludes nothing, so returning an empty slice keeps the behaviour the interface had without the method. Return a copy rather than the field itself: the configuration is read on every request while the caller is free to keep the slice it was handed.

```go
func (instance *CustomHttpConfiguration) StaticExcludedPaths() []string {
	return append([]string{}, instance.staticExcludedPaths...)
}
```

### Compile-level: `cli/output.Option` lost `Fields` and `SortKey`

**What changed.** The `--fields` and `--sort` flags are withdrawn. No printer ever read them and no command ever sorted on a supplied key, so they are gone from the flag set, from [`output.Option`](../cli/output/option.go) and from the `meta.flags` block of the json envelope; `output.SplitFields` is removed with them.

**Symptom.** A custom command that constructed an `output.Option` literal naming `Fields` or `SortKey`, or that called `output.SplitFields`, no longer compiles. At runtime, an invocation passing `--fields` or `--sort` now fails as an unknown flag instead of being silently ignored.

**Remedy.** Drop the fields from the literal and drop the call. A command that genuinely wants a projection or a sort key declares its own flag and applies it to the payload it builds.

### Routing: a non-final optional parameter without a default is refused at registration

**What changed.** An omitted optional parameter is dropped wherever it sits in the pattern, while a match only ever ends early at the tail. A pattern such as `/blog/:locale?/posts` therefore let [`UrlGenerator.GeneratePath`](../http/url_generator.go) mint `/blog/posts`, a path this very router answered with `404`. Such a pattern is now refused at the definition site by [`rejectNonTrailingOptionalParameter`](../http/router.go). A mid-pattern optional that carries a **non-empty default** is still accepted, because the default is always substituted and the segment is therefore never dropped.

**Symptom.** The application no longer boots. Registration panics with `optional route parameter must be the last pattern segment unless it has a default`, and the exception context names the offending `pattern` and `parameterName`.

**Remedy.** One of three, depending on what the route meant:

```go
/* refused at registration: the optional parameter is not the last segment and carries no default */
router.Handle(nethttp.MethodGet, "/blog/:locale?/posts", blogHandler)

/* accepted: the optional parameter is the last segment */
router.Handle(nethttp.MethodGet, "/blog/posts/:locale?", blogHandler)

/* accepted: a mid-pattern optional whose non-empty default is always substituted */
router.HandleWithOptions(
	"/blog/:locale?/posts",
	blogHandler,
	http.NewRouteOptions(
		"blog.posts.localized",
		[]string{nethttp.MethodGet},
		"",
		nil,
		nil,
		map[string]string{"locale": "en"},
		nil,
		0,
		nil,
	),
)

/* accepted: the long and the short pattern registered as two routes */
router.HandleNamed("blog.posts", nethttp.MethodGet, "/blog/posts", blogHandler)
router.HandleNamed("blog.posts.locale", nethttp.MethodGet, "/blog/:locale/posts", blogHandler)
```

An empty default does not lift the refusal: it would emit an empty segment, which no longer satisfies a parameter.

### Routing: a non-empty route default fills in for a parameter supplied empty

**What changed.** [`UrlGenerator.GeneratePath`](../http/url_generator.go) substitutes a non-empty route default for a parameter supplied with an **empty** value, not only for an absent one. A non-trailing optional segment is admitted at registration precisely because its default keeps the segment present, but generating `/:locale?/list/:page` with `{"locale": "", "page": "2"}` dropped it and produced `/list/2` — which this router answers with a `404`, the generator and the matcher disagreeing on the one class of pattern the registration guard newly admits.

**Symptom.** A call that passed an empty string for a parameter that has a non-empty default now gets the default in the path instead of an omitted segment: `/en/list/2` where it used to be `/list/2`. A **required** parameter supplied empty is likewise filled from a non-empty default instead of failing with `route parameter may not be empty`.

**Remedy.** Nothing, in the normal case — the natural caller passes the current locale, which is sometimes `""`, and now gets a path the router actually serves. A caller that relied on an empty value dropping the segment must omit the parameter instead, or give the route no default (or an empty one), which leaves the old behaviour: an optional segment is dropped and a required one is still refused.

### Middleware: equal-priority middlewares run in registration order

**What changed.** [`orderDefinitions`](../http/middleware/pipeline/builder.go) breaks a priority tie on the registration rank instead of on the definition's generated name. The generated name carries the registration counter as decimal text, so a lexicographic tie-break read it as `1, 10, 11, 2` and sorted every factory-provided middleware ahead of every directly registered one. Explicit priorities and `before`/`after` edges decide the order exactly as before.

**Symptom.** The pipeline nests differently. A middleware that used to run outside another may now run inside it, and the reverse. The visible case is a cors middleware registered before an authentication factory at the same priority: it used to end up **inside** the factory, so a preflight was answered `401` with no `Access-Control-Allow-Origin`; it now runs outside it, as the registration order asked.

**Remedy.** If the old nesting was load-bearing, say so explicitly rather than leaning on registration order. A lower priority runs further out:

```go
func (instance *ExampleHttpMiddlewareModule) RegisterHttpMiddlewares(
	kernelInstance kernelcontract.Kernel,
	registrar applicationcontract.HttpMiddlewareRegistrar,
) {
	/* a lower priority runs further out, so cors wraps authentication whatever order the two are registered in */
	registrar.UseWithPriority(-100, cors.DefaultMiddleware())
	registrar.UseWithPriority(0, authenticationMiddleware())
}
```

`before`/`after` edges live on [`pipeline.NewHttpMiddlewareDefinition`](../http/middleware/pipeline/definition.go) for a pipeline assembled directly through [`pipeline.NewBuilder`](../http/middleware/pipeline/builder.go); the module registrar exposes priority. [`(*HttpMiddleware).LastBuildReport`](../application/http_middleware.go) reports the order that was built, and `debug:middleware` renders it.

### Object storage: `awss3` `Put` enforces the declared size

**What changed.** [`Storage.Put`](../../integrations/awss3/v3/storage.go) proves the body against the `size` it was given *before* anything can be committed at the key, and never holds more than 16 MiB in memory doing it. It previously uploaded first and probed the caller's reader afterwards.

**Symptom.** A call that declared a size **shorter** than the body used to report success and leave a truncated object at the key; it now fails with `storage object size does not match the declared size`, naming the key and the declared size, and nothing reaches the key.

Which byte the failure happens at depends on the body:

* A seekable body (`*bytes.Reader`, `*strings.Reader`, `multipart.File`, `*os.File`) is measured in place, so the call fails before a single request is issued.
* A body that cannot seek and is declared at or below 16 MiB is drained and validated in full first, so it also fails before the bucket is touched.
* A body that cannot seek and is declared **above** 16 MiB is checked as MinIO consumes it, so the failure lands partway through the upload: the reader stops one byte short of the declared length, MinIO's multipart upload is aborted, and nothing is visible at the key.

**Remedy.** Declare the real size, or pass `-1` for a stream of unknown length, which is uploaded unchecked:

```go
/* the declared size is enforced, so declare what the body really holds */
putErr := objectStorage.Put(
	runtimeInstance,
	"invoice/2026-07.pdf",
	bytes.NewReader(document),
	int64(len(document)),
	storagecontract.PutOptions{ContentType: "application/pdf"},
)
if nil != putErr {
	return putErr
}

/* a stream of unknown length declares -1 and is uploaded unchecked */
return objectStorage.Put(
	runtimeInstance,
	"upload/report.csv",
	body.Body,
	-1,
	storagecontract.PutOptions{ContentType: "text/csv"},
)
```

A correct size, a zero declared size, and a body **shorter** than its declared size all behave exactly as before. The same pass also stopped reading a legal `(0, nil)` read as the end of the body — which let an over-read go undetected and stored a silently truncated object — and bounds consecutive empty reads while honouring the runtime context, so a stalled body or a client that walked away fails the put instead of pinning a core and an upload.

### Validation: a nil pointer embed is validated as "nothing was supplied"

**What changed.** [`dereferencedValidationStructValue`](../validation/validator.go) yields the zero embed for a nil pointer embed, so the constraints its promoted fields declare run against their zero values exactly as a value embed's already did.

**Symptom.** A request that mentioned no field of a `*T` embed is now rejected with the constraint errors the embed's tags declare. It used to pass: naming any sibling field made `encoding/json` allocate the embed and re-arm the constraints, so a body of `{"status":"open"}` on a request whose `*Audit` embed declares `ActorId` as `notBlank` was accepted and then dereferenced nil in the handler.

**Remedy.** Supply the fields, or stop declaring constraints on an embed the payload is allowed to omit. A promoted field shadowed by an outer field of the same json name stays unvalidated, so the `encoding/json` dominance rules are unchanged.

### Validation: nesting past the depth cap is a validation error when the subtree could carry a tag

**What changed.** Exceeding the nesting-depth cap is reported as [`ErrorNestingDepthExceeded`](../validation/const.go) (`nestingDepthExceeded`) when the truncated subtree could actually carry a `validate` tag, and passes silently when it could not. The walk previously returned an empty error list past the cap, which `Validate` converted to `nil`, so nesting a payload one level deeper than the cap bypassed every constraint in it. The reachability check follows pointers, slices, arrays and map elements and is memoized per type; the cap value is unchanged.

**Symptom.** A deeply nested payload that used to validate now fails with a `nestingDepthExceeded` error naming the field. Tag-free free-form client json — a `map[string]any` metadata field, for example — is still accepted at any depth.

**Remedy.** Flatten the request type, or keep the deep part of the payload tag-free so nothing below the cap declares a constraint.

### Validation: a parameterized constraint is constructed once and shared

**What changed.** The parsed `validate` tag and the constraint a parameterized rule resolves to are memoized instead of being rebuilt for every value the validator reaches — a `regex` tag recompiled its pattern once per element. The parse cache is keyed on the tag string, the constraint cache on the rule name and its parameters, and the constraint cache is per-validator, so custom constraints registered under the same name in different validators stay separate.

**Symptom.** A custom [`contract.ParameterizedConstraint`](../validation/contract/constraint.go) whose `WithParams` result carried per-value state, or was not safe for concurrent use, now leaks that state between unrelated values and unrelated requests.

**Remedy.** Make the constraint `WithParams` returns immutable and safe for concurrent use; do not retain the params map it was handed, and do not accumulate state in `Validate`. One instance is shared for the process lifetime across every request and goroutine that reaches the rule.

### HTTP kernel: `SetSessionCookiePolicy` keeps the `SameSite=Lax` default

**What changed.** [`resolveSessionCookieSameSite`](../http/router_utility.go) treats the zero `SameSite` as unset and falls back to `Lax`, the same way an empty `Path` falls back to `/`.

**Symptom.** A policy that named only `Path` or `Domain` used to emit no `SameSite` attribute at all; it now emits `SameSite=Lax`.

**Remedy.** None, unless the omission was deliberate — `nethttp.SameSiteDefaultMode` remains the way to ask for no attribute on purpose.

### HTTP kernel: the session saved is the one published on the request

**What changed.** [`republishedSession`](../http/router_utility.go) reads `RequestAttributeSession` at the moment the response is written, preferring the session a handler published over the one the kernel captured before routing.

**Symptom.** Replacing that attribute in a handler now takes effect: the published session is what gets stored and what the `Set-Cookie` advertises. It used to be discarded.

**Remedy.** None for a handler that wanted that. A handler that put something else under `RequestAttributeSession` as scratch space must stop — the constant is framework-owned and the response path acts on it.

### HTTP: `JsonHandler` rejects a literal `null` body

**What changed.** [`JsonHandler`](../http/typed_handler.go) answers `400` for a literal `null` request body when its request type is instantiated as a pointer. The four-byte body decoded without error and left the value nil, the validator took its nil-pointer early return and reported every constraint satisfied, and the handler then dereferenced nil.

**Symptom.** That request is now a client error instead of a `500`.

**Remedy.** None. A value instantiation and a `{}` body were never affected, and a caller-supplied [`WithJsonHandlerErrorResponder`](../http/typed_handler.go) still shapes the response.

### HTTP client: `MaxIdleConnsPerHost` is set on the transport

**What changed.** [`TransportConfig.MaxIdleConnsPerHost`](../httpclient/transport_config.go) is exposed and defaults to `MaxIdleConns` (100), following an override of it unless pinned explicitly. It was never set, so `net/http` fell back to `DefaultMaxIdleConnsPerHost` (2) and the configured `MaxIdleConns: 100` was inert.

**Symptom.** Connection reuse against a single host now scales with `MaxIdleConns` instead of stopping at two. Idle sockets to one upstream are held rather than closed, so the process keeps more open file descriptors and the upstream sees more long-lived connections. The old behaviour exhausted the ephemeral port range under a burst — every connection past the second closed straight into `TIME_WAIT` — and reported `connect: cannot assign requested address` as `"request failed"`.

**Remedy.** Nothing, in the normal case. A caller who relied on the two-connection ceiling, or whose upstream caps connections per client, sets `MaxIdleConnsPerHost` explicitly.

### CLI: json mode writes the document and nothing else

**What changed.** In json mode the ansi start/finish banner that [`cli.Register`](../cli/command.go) wraps around every registered command is suppressed, and `--format=json` implies `--no-color` through [`NormalizeOption`](../cli/output/option_parser.go).

**Symptom.** `jq` and `json.Unmarshal` now consume `debug:*` output directly instead of failing on the first byte. A consumer that scraped the banner off stdout finds it gone.

**Remedy.** Read the envelope. `meta` already reports the command, its arguments, the start time and the duration, and `error` reports the final status.

### CLI: a command whose envelope reports an error exits non-zero

**What changed.** [`output.Render`](../cli/output/renderer.go) returns an exit-coded error after writing the envelope. A registered service that errors or panics while being constructed is reported as `debug.buildFailed` rather than `debug.notFound`.

**Symptom.** A command that reported an error in its payload while exiting `0` now exits `1`. `debug:container <name>` fails when the service cannot be resolved instead of printing `[success]`.

**Remedy.** Nothing to change in the framework. A wrapper script that treated a zero exit as success was reading a status that was never true; a deployment gate such as `app debug:container app.repository.order || exit 1` now works as written. A command of your own that renders a non-nil `Envelope.Error` deliberately and still wants a zero exit must not put the failure in the envelope.

### CLI: `--format` and `--order` reject an unrecognised value

**What changed.** Both flags carry a validator ([`StandardFlags`](../cli/output/standard_flag.go)), so `--format=JSON`, `--format=yaml` and `--order=ascending` fail during flag parsing with a message naming the accepted values, matching how `--limit=abc` already behaved.

**Symptom.** A script passing an unsupported value now fails with a non-zero exit instead of quietly receiving the human table.

**Remedy.** Pass `table` or `json`, and `asc` or `desc`. Omitting either flag still defaults to `table` and `asc`.

### CLI: `--limit`, `--offset` and `--order` are applied to the rendered items

**What changed.** `debug:router`, `debug:events`, `debug:parameters`, `debug:middleware` and `debug:container` apply the window through [`output.WindowItems`](../cli/output/list_payload.go) and the order through [`output.ApplySortOrder`](../cli/output/list_payload.go), reversal running before the window so a descending window returns the end of the list. `total` continues to report the unwindowed count.

**Symptom.** An invocation already passing `--limit` or `--offset` received the full list and now receives a window; with `--verbose`, `debug:events` also narrows its listeners block to the windowed events. `--order=desc` was accepted and ignored before, so an invocation that passed it now gets different output.

**Remedy.** Nothing for a client that paged with `offset += limit` — it now walks each item exactly once instead of re-reading the whole list on every page. A consumer that passed `--limit` while expecting everything must drop the flag.

### Distributed lock: `LeaderGate.OnElected` receives a term-scoped runtime

**What changed.** [`LeaderGate`](../lock/leader_gate.go) starts renewing its lease **before** `OnElected` runs, and hands the hook a runtime whose context is cancelled when the lease is lost. Nothing renewed the lease while the hook ran, so a hook slower than the ttl let the lease lapse: another replica acquired it, both reported leadership, and the incumbent never demoted, because demotion only follows a failed renewal — which could not happen while the hook held the campaign loop.

**Symptom.** Leader-only work started inside `OnElected` that respects its context now stops when the lease is lost, instead of running alongside the new holder. The runtime the hook receives is no longer the run runtime, so its context ends at the end of the term rather than at the end of the process.

**Remedy.** If the hook needs a context that outlives the term — a cleanup that must finish whatever happens to the leadership — derive it from the run runtime captured outside the hook rather than from the one the hook is handed. Work that is only correct while this replica is leader should keep using the hook's runtime, which is the point of the change.

### Message bus: `melody:messagebus:consume` separates the signal from the handler lifetime

**What changed.** [`ConsumeCommand`](../messagebus/consume_command.go) runs the delivery pull and the handlers on two contexts. The shutdown signal stops the pull; in-flight handlers and their `Ack`/`Nack` keep a live context for the whole [`WithShutdownGrace`](../messagebus/consume_command.go) window (30 seconds by default), and the grace expiring is what cancels them.

**Symptom.** A handler that respects its context is no longer cancelled the instant the signal arrives; it is cancelled when the grace expires. One shared context meant the grace protected nothing, and the acknowledgement ran on the cancelled context too — so any transport honouring the runtime context on publish failed the `Ack` of a message whose side effects had already committed, the broker redelivered it on every deploy, and a failed `Nack` dropped the `RedeliveryStamp` increment so `MaxRetries` never converged.

**Remedy.** Size the grace to the slowest handler with `WithShutdownGrace`, and make sure a handler that must not be interrupted mid-write finishes inside it. A handler that relied on being killed at the signal now runs to completion or to the grace deadline.

### Websocket: a zero `IdleTimeout` is refused at construction

**What changed.** [`websocket.NewStreamHandler`](../../integrations/websocket/v3/handler.go) panics when `Options.IdleTimeout` is not positive, rather than treating the zero value as "no keepalive". Nothing else in the stack can reap a peer that goes away without a fin: `coderwebsocket.Accept` hijacks the connection, so `http.Server`'s read and write timeouts stop applying to it; the read loop then blocks in `Read` with no deadline of its own; and a write into a half-open socket keeps succeeding for as long as the send buffer has room, so a broadcast is no liveness signal either. The keepalive ping is the only remaining evidence, which makes its interval a required decision rather than a tunable with an off position. Left at zero, connections opened and abandoned each cost a descriptor, a hub subscription and three goroutines for the life of the process.

**Symptom.** An application whose websocket options never named an `IdleTimeout` no longer starts. The construction panics with `websocket options require a positive IdleTimeout: ...`, and the exception context carries the `idleTimeout` it was given. Through [`websocket.NewModule`](../../integrations/websocket/v3/module.go) the handler is built while routes are registered, so the failure surfaces at boot rather than on the first upgrade request — deliberately, the way the framework reports every other unusable configuration.

**Remedy.** Name the interval at which a silent peer should be pinged. `30s` suits a browser client, which answers the ping inside its protocol stack where the page's JavaScript never sees it, so a receive-only client stays connected:

```go
websocket.NewModule(websocket.ModuleConfig{
	Hub:  hub,
	Path: "/ws",
	Options: websocket.Options{
		TopicResolver: topicResolver,
		WriteTimeout:  10 * time.Second,
		IdleTimeout:   30 * time.Second,
	},
})
```

The module supplies no default of its own on purpose: the only thing that reaps a vanished peer should be chosen by the application rather than inherited silently.

### Other integration modules

* **`bunorm` deterministic encryption.** `melody:encrypt:database --mode=encrypt --deterministic` ([`encrypt_database_command.go`](../../integrations/bunorm/v3/encrypt/encrypt_database_command.go)) now rewrites a column that was already bulk-encrypted with random nonces into its deterministic form, keeping the key each value already carries ([`migrate.go`](../../integrations/bunorm/v3/encrypt/migrate.go)). Every such value previously authenticated under a live key and was passed through untouched, so the command reported success while converting nothing and every [`CiphertextCandidates`](../../integrations/bunorm/v3/encrypt/cipher.go) equality lookup on that column returned zero rows. *Symptom:* a deterministic run over an already-encrypted column now writes rows where it used to write none. *Remedy:* none — it remains idempotent and never rotates keys, so `--mode=reencrypt --target-key=...` is still the only way to change a key.
* **`bunorm` audit change-sets.** [`audit.ChangeSet`](../../integrations/bunorm/v3/audit/change.go) always serialises an empty change-set as `[]` rather than the json literal `null`. *Symptom:* a trail consumer that special-cased `null` in the `changes` column sees `[]` instead. *Remedy:* drop the special case; `jsonb_array_length(changes::jsonb)` now reads `0` where it errored or read `1`.
* **`websocket` keepalive.** A received pong refreshes the connection's liveness mark, and a keepalive ping that could not be written because a data frame was in flight is no longer read as a dead peer ([`handler.go`](../../integrations/websocket/v3/handler.go)). *Symptom:* a configuration with `IdleTimeout` below `WriteTimeout` no longer turns transient write contention into a disconnect — a frame in flight excuses a timed-out ping until one interval past the configured write timeout. *Remedy:* none; a receive-only client bridged onto a broadcast hub stops being disconnected for never sending a data frame.
* **`outbox` relay lease.** The distributed lease is released on a context detached from the run and bounded by five seconds, and a release failure is logged rather than discarded ([`relay.go`](../../integrations/outbox/v3/relay.go)). *Symptom:* a graceful restart no longer stalls outbox delivery for a whole `LockTtl`. *Remedy:* none.

### Wiring: the generator refuses what it used to drop silently

**What changed.** `melody:wiring:generate` fails, naming the site, on the inputs it used to read as "nothing": an unknown `//melody:` directive (a mistyped `scoped` demoted a request-lifetime service to a never-closed singleton; a mistyped `ignore` registered the constructor it acknowledged), a `//melody:bind` assignment without the equals sign or with an empty half (the override beside the constructor silently fell back to a broader bind), a malformed exclude pattern (`path.Match`'s `ErrBadPattern` was read as "does not match", so the exclusion excluded nothing), an empty import path or directory on a package binding (an empty directory scanned the whole project tree as one package), and two constructors that would register the same container key (the generated file panicked at first boot while the generation had reported success). `//melody:ignore` now accepts a trailing reason, which is the spelling the refusal of unknown directives makes mandatory to honour. An exclude that matched no constructor is reported like an unused bind, `--strict` fails on it, and a strict refusal carries every violation — binds, excludes, skipped constructors — in one error instead of the first found.

**Symptom.** A generation that used to succeed over a tree carrying any of these now fails with an error naming the file and line, and a `--strict` pipeline with a dead exclude goes red.

**Remedy.** Correct the named site: fix the directive spelling, add the equals sign, terminate the character class, split the two constructors or route one through `//melody:ignore`. Every refusal is a defect the generated file would otherwise carry into boot — none of them is a new rule about correct input.

### Wiring and openapi: the `--out` contract hardens, and the openapi anchor moves

**What changed.** Both generate commands write through a temp file and a rename, so an interrupted write leaves the previous artifact intact instead of a torn one; both refuse to replace a file that is not theirs — wiring by the `DO NOT EDIT` marker, openapi by the target not holding a JSON document; wiring refuses an `--out` inside a scanned package directory, which its own documentation always forbade; and a relative `--out` on `melody:openapi:generate` is now anchored at the project directory, exactly as the wiring command has always anchored its own, with the parent directories created on the way.

**Symptom.** `melody:openapi:generate --out openapi.json` run from a working directory other than the project root — a systemd unit, a Makefile in a subdirectory — now writes into the project instead of into that directory; a pipeline that relied on the old CWD anchoring reads the file from the wrong place. A mistyped `--out` pointing at a hand-written file fails instead of destroying it.

**Remedy.** Pass an absolute `--out` to pin any other destination; delete a foreign file deliberately if its path really is the intended output. Nothing changes for the documented invocations run from the project root.

### Openapi: the document's shape becomes faithful to the router, and stable

**What changed.** A route registered with no method list — which the router answers on every verb — is documented on all eight path item verbs instead of as an operation-less `{}`; a verb the format cannot model (`PURGE`) is named in the path item's new `description` instead of vanishing; a catch-all pattern stops the documented path at the catch-all segment, because the router discards everything after it; converging routes no longer overwrite each other's operations (the earlier registration wins, as in the router's match order); and response types are visited in status order, so component names and every `$ref` to them stop depending on map iteration and the generated file is byte-stable across runs.

**Symptom.** A committed `openapi.json` regenerated after this change may differ once — method-less routes gain operations, catch-all paths shorten, colliding component names settle onto the lower status — and then stays byte-identical run over run, which is the property the diff-based pipelines were missing.

**Remedy.** Regenerate the committed document once and review the diff; it is the document moving onto what the router actually serves. Declare explicit method lists on routes that should not advertise all eight verbs.

## v3.0.0

v3 is a separate import path, so an application moves onto it by rewriting its imports rather than by resolving a new version. The entry below is the one rewrite that does not compile afterwards: v1 and v2 keep the identifiers, v3 has never carried them.

### Compile-level: `validation` does not carry the twelve deprecated constants

**What changed.** [`validation/const.go`](../validation/const.go) declares `ServiceValidator`, `ErrorInvalidRuleSyntax`, `ErrorUnknownRule` and `ErrorNestingDepthExceeded` and nothing else. The twelve deprecated aliases that v1 and v2 still declare are absent. Each one was defined as the constant that replaces it, so every replacement carries the identical string and the rewrite is a rename:

| Absent in v3           | Replacement                                                                              | Value                |
|------------------------|------------------------------------------------------------------------------------------|----------------------|
| `ErrorNotAlpha`        | [`ConstraintAlphaErrorNotAlpha`](../validation/constraint_alpha.go)                      | `notAlpha`           |
| `ErrorNotAlphanumeric` | [`ConstraintAlphanumericErrorNotAlphanumeric`](../validation/constraint_alphanumeric.go) | `notAlphanumeric`    |
| `ErrorInvalidEmail`    | [`ConstraintEmailErrorInvalidEmail`](../validation/constraint_email.go)                  | `invalidEmail`       |
| `ConstraintMax`        | [`ConstraintMaxLength`](../validation/constraint_max_length.go)                          | `max`                |
| `ErrorMaxLength`       | [`ConstraintMaxLengthErrorTooLong`](../validation/constraint_max_length.go)              | `tooLong`            |
| `ConstraintMin`        | [`ConstraintMinLength`](../validation/constraint_min_length.go)                          | `min`                |
| `ErrorMinLength`       | [`ConstraintMinLengthErrorInsufficientLength`](../validation/constraint_min_length.go)   | `insufficientLength` |
| `ErrorNotBlank`        | [`ConstraintNotBlankErrorIsBlank`](../validation/constraint_not_blank.go)                | `isBlank`            |
| `ErrorEmpty`           | [`ConstraintNotEmptyErrorEmpty`](../validation/constraint_not_empty.go)                  | `empty`              |
| `ErrorNotNumeric`      | [`ConstraintNumericErrorNotNumeric`](../validation/constraint_numeric.go)                | `notNumeric`         |
| `ErrorRegexMismatch`   | [`ConstraintRegexErrorMismatch`](../validation/constraint_regex.go)                      | `regexMismatch`      |
| `ErrorInvalidPattern`  | [`ConstraintRegexErrorInvalidPattern`](../validation/constraint_regex.go)                | `invalidPattern`     |

`ConstraintMax` and `ConstraintMin` are rule names — the token a `validate` tag spells — and the other ten are error codes a client reads off a validation failure.

**Symptom.** Code that named any of them stops compiling with `undefined: validation.ErrorNotAlpha` and the like. The failure is per identifier, so a package that used several reports several.

**Remedy.** Rename each reference to the replacement column above:

```go
/* v1 / v2 */
if validation.ErrorNotBlank == validationError.Code() {

/* v3 */
if validation.ConstraintNotBlankErrorIsBlank == validationError.Code() {
```

Nothing outside the Go source changes: the strings are identical, so a `validate` tag, an api client matching on the error code, and a translation catalogue keyed on it all keep working untouched.
