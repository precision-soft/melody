# Cron generator integration

This directory contains the **optional cron generator integration** for Melody.

A declarative `cron.Configuration` registry maps CLI command names to schedules. A single meta command (`melody:cron:generate`) consumes that registry and writes a deterministic crontab file. Commands themselves stay plain Melody CLI commands — they do not implement any cron-specific interface, so a command's implementation and its scheduling concerns live in different files.

The integration is published as independent Go modules so applications pull only the binding matching their melody version:

* Melody v1 binding: this directory — `github.com/precision-soft/melody/integrations/cron`
* Melody v2 binding: [`./v2/`](./v2/) — `github.com/precision-soft/melody/integrations/cron/v2`
* Melody v3 binding: [`./v3/`](./v3/) — `github.com/precision-soft/melody/integrations/cron/v3`

The three bindings share the same core exported API and behavior; they differ in the melody version they import, and in a handful of identifiers each side has and the other does not — v3 ships the built-in `k8s` template (see [Customizing the template](#customizing-the-template)) with its errors and parameters, plus a `Commands` helper, while v1 and v2 ship the ownership and user-column surface v3 dropped (see [Package surface](#package-surface) for both lists). The examples below use the v3 import path; for v2, replace `/v3` with `/v2`; for v1, drop the `/v3` suffix entirely (the v1 binding lives at the module root, following Go's no-suffix convention for v0/v1).

## What you get

* A declarative **configuration registry** ([`cron.Configuration`](./configuration.go)) — userland code maps a command name to an `EntryConfig` (schedule + per-command overrides) via a fluent builder: `cron.NewConfiguration().Schedule(commandName, &cron.EntryConfig{...}).Schedule(...)`.
* A type-safe **command-name helper** (`cron.CommandName[T](constructor)`) — instantiates a constructor and returns its `Name()` so the registry references commands by constructor instead of hardcoded strings.
* A `melody:cron:generate` command ([`cron.GenerateCommand`](./generate_command.go)) constructed via `cron.NewGenerateCommand(configuration)`.
* A `cron.Render(entries, options) (string, error)` renderer (free-function wrapper in [`./template.go`](./template.go) that dispatches to the built-in crontab template in [`./template_crontab.go`](./template_crontab.go)) that validates entries and produces the crontab text. New output formats are added by dropping a new `template_<format>.go` next to `template_crontab.go`, implementing the `cron.Template` interface, and registering the constructor in `BuiltinTemplates()`.
* **Container-parameter cascade** ([`./parameter.go`](./parameter.go)) — every flag falls back to a configurable parameter.
* **Per-command overrides** on `EntryConfig`: each entry can pick its own destination file, custom command parts, log file name, instance count, or disable logging entirely.
* **Custom heartbeat command**: `RenderOptions.HeartbeatCommand` replaces the default `/bin/touch <path>` heartbeat line with any command shape (e.g. an HTTP ping).

## Binary resolution

The crontab entries reference the **built application binary** itself (resolved via `os.Executable()` at run time, or overridden with `--binary` / `melody.cron.binary`). Run `melody:cron:generate` from the production binary — invoking it through `go run` resolves the binary to a temporary build path that disappears once the process exits, which is almost never what you want in `crontab`. The fallback also resolves symlinks: `os.Executable()` reads the running binary's real path, so on a deployment that publishes releases behind a `current` symlink the entries pin the release directory the generator ran from — the manifest keeps executing that release's binary for as long as the directory survives, and answers `No such file or directory` once it is pruned. On that deployment shape, pass the symlinked path explicitly through `--binary` or `melody.cron.binary`.

## Install

Pick the binding that matches your melody version:

```bash
# Melody v1
go get github.com/precision-soft/melody/integrations/cron@latest

# Melody v2
go get github.com/precision-soft/melody/integrations/cron/v2@latest

# Melody v3
go get github.com/precision-soft/melody/integrations/cron/v3@latest
```

Each binding depends on its respective melody module — `github.com/precision-soft/melody` for v1, `github.com/precision-soft/melody/v2` for v2, `github.com/precision-soft/melody/v3` for v3 — plus `github.com/urfave/cli/v3` (the urfave/cli major version is shared across all three bindings).

## Configuration parameters

The cascade for every flag is **CLI flag (explicitly set) → container parameter**. Two flags carry a final fallback inside the command itself: `--template` falls back to the crontab dialect and `--binary` falls back to the running executable's own path (see the table below). Every other flag has no hardcoded fallback; those defaults live entirely in the parameter system.

The parameter names are exposed as constants:

| Constant                             | Parameter name                  | CLI flag                  | Default from `RegisterDefaultParameters`                                                                                                                                                                                                                                                                                         |
|--------------------------------------|---------------------------------|---------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `cron.ParameterDestinationFile`      | `melody.cron.destination_file`  | `--out`                   | `%kernel.project_dir%/generated_conf/cron/crontab`                                                                                                                                                                                                                                                                               |
| `cron.ParameterLogsDir`              | `melody.cron.logs_dir`          | `--logs-dir`              | `%kernel.logs_dir%/cron`                                                                                                                                                                                                                                                                                                         |
| `cron.ParameterUser`                 | `melody.cron.user`              | `--user`                  | _not registered_ — userland must supply a user via `--user`, `melody.cron.user`, or per-command `EntryConfig.User`, otherwise generation fails with `ErrEntryEmptyUser` (or `ErrHeartbeatUserMissing` when a heartbeat is configured)                                                                                            |
| `cron.ParameterBinary`               | `melody.cron.binary`            | `--binary`                | _not registered_ — falls through to `os.Executable()` when both flag and parameter are empty                                                                                                                                                                                                                                     |
| `cron.ParameterHeartbeatPath`        | `melody.cron.heartbeat_path`    | `--heartbeat-path`        | _not registered_ — heartbeat disabled by default                                                                                                                                                                                                                                                                                 |
| `cron.ParameterHeartbeatAutoEnabled` | `melody.cron.heartbeat.enabled` | _no flag_                 | _not registered_ — when truthy (parsed by `Parameter.Bool()`: `true`/`1`/`yes`/`on`, case-insensitive, trimmed) **and** neither `--heartbeat-path` nor `melody.cron.heartbeat_path` is set, the heartbeat path defaults to `<logs-dir>/heartbeat.crontab`. See [Auto-derive the heartbeat path](#auto-derive-the-heartbeat-path) |
| `cron.ParameterTemplate`             | `melody.cron.template`          | `--template`              | `crontab` (set by `RegisterDefaultParameters`)                                                                                                                                                                                                                                                                                   |
| _no parameter_                       | _no parameter_                  | `--heartbeat-command`     | repeatable; each value is one argv token of a custom heartbeat command. When set, overrides `--heartbeat-path`                                                                                                                                                                                                                   |
| _no parameter_                       | _no parameter_                  | `--heartbeat-destination` | repeatable; restricts the heartbeat to the listed destinations. Values: `default` (the `--out` file), an absolute path, or a relative path matched against `dir(--out)`. When unset, the heartbeat goes to every destination                                                                                                     |

A parameter is looked up when the matching CLI flag was not explicitly set (urfave's `IsSet`) **or** when it was set to an empty value: an explicit `--user=` carries no value to use, so it falls through to `melody.cron.user` rather than forcing an empty result. Only a flag that is both set and non-empty short-circuits the cascade ([`resolveDefault`](./generate_command.go)).

`--heartbeat-command` and `--heartbeat-destination` are CLI-only — they have no container-parameter fallback. The other flags (`--out`, `--logs-dir`, `--user`, `--binary`, `--heartbeat-path`, `--template`) cascade through the parameter system as described above.

## Cron expression validation

The generator parses every `Schedule.Minute / Hour / DayOfMonth / Month / DayOfWeek` field against the crontab grammar and the bounds the target scheduler enforces, and **fails generation** when a field does not parse. A daemon treats one bad field as a parse error and refuses the *whole* crontab file with it — dropping every entry in that file, not just the offending one — so a bad field is caught before it can be written.

The field-level checks performed at generation time are:

* any Unicode whitespace anywhere in a field is rejected — crontab fields must be single tokens (a vertical tab or a no-break space splits a crond line exactly as a plain space does);
* `%`, `\n`, `\r` are rejected anywhere in a rendered token (they would split or escape the line);
* a step on a single value (`5/2`) is rejected — the target schedulers disagree on that shape, so the explicit range must be written instead;
* the field must parse as `*`, a number, a `low-high` range, a comma-separated list of those, and an optional `/step`, with every value inside the field's bounds.

The bounds are `Minute` 0–59, `Hour` 0–23, `DayOfMonth` 1–31, `Month` 1–12, `DayOfWeek` 0–7 (7 aliases Sunday; the Kubernetes dialect bounds it at 6, as the robfig scheduler behind the `k8s` template does). Three-letter month and day names (`jan`, `mon`, …) are folded onto their numbers as complete range endpoints; a name glued to digits (`1jan`) or in step position stays put and fails the parse, exactly as the daemons refuse it. A whole-field `?` in a day field is accepted under the **kubernetes runner dialect**, not by a template: it is folded to `*` in the schedule matcher (the Quartz convention the robfig scheduler reads as the wildcard), and the crontab dialects reject it. The generator judges every field against the crontab bounds whatever dialect the runner uses — see the paragraph below. A refusal names the field it refused, the bounds it judged the value against and — where the runner dialect chose those bounds, which is the day-of-week field — the dialect: `DayOfWeek: "7"` is a legal Sunday under the crontab dialect and out of range under `RunnerDialectKubernetes`, and the record has to say which rule was applied.

So each of `Minute: "99"`, `Hour: "abc"`, `DayOfMonth: "*/0"` and an unbalanced range like `"5-3"` is a **hard error** — `melody:cron:generate` reports `cron: entry "X" has an invalid Schedule.<field> ("...")` (wrapping [`ErrInvalidSchedule`](./errors.go)) and generation fails. On a single-destination run — the common case — that means no file is written at all. Note the multi-destination caveat below: schedule validation runs inside `Template.Render`, and `melody:cron:generate` renders and writes **one destination at a time**, so with entries routed to several files via `EntryConfig.DestinationFile` a bad schedule on a later destination leaves the earlier ones already written. A step wider than the field is the one permissive corner: it is clamped to the field's cardinality rather than rejected, because crond accepts it and admits the range's low value alone.

The validation deliberately mirrors what the *generated artifact's* scheduler accepts, not one canonical grammar. The in-process runner shares the field rules with one deliberate exception: under the kubernetes dialect the runner reads a whole-field `?` as `*`, while the generator judges every field against the crontab bounds whatever dialect the runner uses — so a `?` the kubernetes runner accepts is still refused by `melody:cron:generate`.

`melody:cron:generate` errors out when:

* `melody.cron.destination_file` (and `--out`) are both empty — `cron: no output path configured`
* a command has `LogDisabled=false` but `melody.cron.logs_dir` (and `--logs-dir`) are both empty — `cron: command wants log redirection but no logs-dir is configured; set --logs-dir, register the melody.cron.logs_dir parameter, or set EntryConfig.LogDisabled=true`, with the command's name carried in the error context
* a rendered entry ends up with an empty `User` — `cron: command "X" has no user; set EntryConfig.User on the schedule, pass --user, or register the melody.cron.user parameter`

### Pre-built defaults via `RegisterDefaultParameters`

The integration ships an opt-in helper that registers safe path/template defaults from the table above (`%kernel.project_dir%` and `%kernel.logs_dir%` are resolved by melody automatically — see [Project directory resolution](#project-directory-resolution-uncompiled-vs-compiled) below). `melody.cron.user` is **not** registered by the helper because there is no sensible default — the integration has no idea which OS user should run your crontab. Userland must register it (or supply `EntryConfig.User` / `--user`).

```go
import (
melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
melodycron "github.com/precision-soft/melody/integrations/cron/v3"
)

func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
melodycron.RegisterDefaultParameters(registrar)

registrar.RegisterParameter(melodycron.ParameterUser, "%env(COMMANDS_USER)%")
}
```

### Custom defaults via env-driven parameters

If you'd rather wire each parameter to your own `.env` variables, skip the helper and register them directly:

```sh
# .env
COMMANDS_USER=www-data
COMMANDS_LOGS_DIR=%kernel.logs_dir%/cron
```

```go
func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
registrar.RegisterParameter(melodycron.ParameterUser, "%env(COMMANDS_USER)%")
registrar.RegisterParameter(melodycron.ParameterLogsDir, "%env(COMMANDS_LOGS_DIR)%")
// melody.cron.destination_file omitted — supply via --out at run time or register here
}
```

Parameter resolution is recursive: `COMMANDS_LOGS_DIR=%kernel.logs_dir%/cron` reads the built-in `kernel.logs_dir` parameter and substitutes it before the value reaches the cron command.

### Project directory resolution (uncompiled vs compiled)

melody computes `kernel.project_dir` at boot:

* **Uncompiled** (`go run .`) — finds the project root by climbing up from the working directory until a `go.mod` is found. The crontab and log paths use that root, so `melody:cron:generate` writes under your source tree.
* **Compiled binary** — uses the directory containing the binary (resolved through `os.Executable()` + `EvalSymlinks`). The crontab and log paths use that directory, which is typically the deployment root.

That means `%kernel.project_dir%/generated_conf/cron/crontab` works unchanged in both modes; you don't need different defaults for dev and prod.

A path that is relative anyway is anchored by where it came from: a value read from a **parameter** — `melody.cron.destination_file`, `melody.cron.logs_dir`, `melody.cron.heartbeat_path` — is resolved against `kernel.project_dir`, the way melody resolves `MELODY_LOG_PATH`, `kernel.logs_dir` and `kernel.cache_dir`, while a path passed as a **cli flag** stays relative to the working directory, which is what a shell means by it. So `melody.cron.logs_dir = var/log/cron` names one directory whatever the deploy step's working directory is, and `--logs-dir var/log/cron` follows the caller. An absolute value is never re-anchored, and a configuration whose `Kernel()` names no project directory keeps the working-directory anchoring.

#### Production workflow

Build and run the binary from its deployment root:

```sh
go build -o /opt/myapp/myapp .
/opt/myapp/myapp melody:cron:generate
# writes /opt/myapp/generated_conf/cron/crontab with entries pointing at /opt/myapp/myapp
```

#### Development workflow (`go run .`)

`go run` builds an ephemeral binary under `$GOCACHE/.../go-build*/...`. The output crontab and log paths still resolve correctly via the project root (because the cron command falls through to "find the nearest `go.mod`"), but the binary path inside each entry points at the temporary build, which evaporates as soon as the process exits.

For a usable crontab in dev mode, pass `--binary` (or register `melody.cron.binary`) so the entries reference your deployed binary path instead:

```sh
go run . melody:cron:generate --binary=/opt/myapp/myapp
# writes <project>/generated_conf/cron/crontab with entries pointing at /opt/myapp/myapp
```

Both modes have been verified end-to-end with a minimal melody v3 application.

## Per-command overrides

An `EntryConfig` value can opt in to additional per-command behavior beyond the cron expression itself:

| Field             | Effect                                                                                                                                                                                                                                                                                                 |
|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Schedule`        | pointer to the 5-field `*cron.Schedule` (Minute, Hour, DayOfMonth, Month, DayOfWeek). When `nil`, the entry runs every minute (`* * * * *`)                                                                                                                                                            |
| `User`            | crontab manifests: run this entry as a specific system user instead of the default `--user`. In-process runner: ignored with one warning — every job runs as the process user                                                                                                                                                                                                                               |
| `LogFileName`     | use a custom file name (within `--logs-dir`) instead of the sanitized command name                                                                                                                                                                                                                     |
| `LogFileNameRaw`  | when auto-deriving the log file name (no `LogFileName` set), keep `:` and other non-`/` characters; only `/` is replaced for filesystem safety. Default behavior sanitizes `:` → `-`                                                                                                                   |
| `LogDisabled`     | omit the `>> '<log>' 2>&1` redirection entirely for this entry                                                                                                                                                                                                                                         |
| `DestinationFile` | route this entry to a different crontab file (relative paths join `dir(--out)`; absolute paths are used verbatim). The generator writes one file per distinct destination and emits the heartbeat into each                                                                                            |
| `Command`         | replace `<binary> <command-name>` with a custom argv slice (e.g. wrap with `/usr/bin/flock`, `nice`, `php`, or substitute the whole command). When set, the `--binary` cascade is ignored for this entry                                                                                               |
| `Arguments`       | the entry's own command-line arguments, appended after the command name wherever this entry runs: the generated manifest line carries them, and the **in-process runner** hands them to the child command. This is where a job declares its own output posture — `Arguments: []string{"--format=json"}` — since the runner has none to lend it. Ignored when `Command` replaces the whole argv |
| `Instances`       | when set to `N > 1`, the generator expands the schedule into `N` entries with `--max-instances=N --instance-index=I` flags appended to the default args (skipped when `Command` is set) and a per-instance `-I` suffix on the log file. Use it to parallelize the same command across multiple workers |
| `Timeout`         | bounds one run of this entry under the **in-process runner** (`melody:cron:run`); the generated manifests ignore it. Zero takes the runner default, which is **no deadline at all** — nothing else bounds a run, so an entry that wants the bound asks for it here, and an hour is a reasonable value for work that normally finishes in seconds; a negative value opts out of the deadline deliberately rather than by omission. A command cancelled by the deadline is reported wrapping `ErrCommandTimeout`; one that never returns is abandoned one `GracefulTimeout` later, reported, and its container scope released — otherwise a command wedged on a deadline-less read would hold a goroutine and a scope per matching minute for the life of the process |
| `GracefulTimeout` | the window between the deadline cancelling the command's context and the run being abandoned with its scope closed under it. Zero takes the runner default of **five minutes**, long enough for an honest unwind — flushing a batch, rolling a transaction back; without a `Timeout` deadline the window is never reached                                                                                                                                                                                                                       |

Example:

```go
cronConfiguration.Schedule("billing:run", &melodycron.EntryConfig{
Schedule:        &melodycron.Schedule{Minute: "0", Hour: "2"},
User:            "billing",
DestinationFile: "billing-crontab",
Command:         []string{"/usr/bin/flock", "-n", "/var/run/billing.lock", "/opt/app", "billing:run"},
LogFileName:     "billing.log",
})
```

## Auto-derive the heartbeat path

Set `melody.cron.heartbeat.enabled` (constant: `cron.ParameterHeartbeatAutoEnabled`) to a truthy value to opt in to auto-deriving the heartbeat path from `--logs-dir` whenever neither `--heartbeat-path` nor `melody.cron.heartbeat_path` is set. The value is parsed by the framework's `Parameter.Bool()` helper, which trims whitespace and accepts `true`/`false`, `1`/`0`, `yes`/`no`, `y`/`n`, `on`/`off` (case-insensitive). An **unregistered** parameter leaves the heartbeat disabled; a registered parameter holding the empty string or any other unrecognised value **fails the generation** with `cron: the heartbeat opt-in parameter does not hold a boolean`, because a malformed opt-in silently read as "off" would generate a crontab without the liveness line and report success. The parameter is read only where it could matter — when no heartbeat path was given **and** a logs directory was — so a run with no logs directory generates successfully without a liveness line and never consults the opt-in at all, malformed or not: there was no path to derive from in the first place.

The parameter only controls the auto-derive fallback; it does **not** gate the heartbeat when an explicit path is set. An explicit `--heartbeat-path` flag or `melody.cron.heartbeat_path` parameter always emits a heartbeat, regardless of `melody.cron.heartbeat.enabled`.

```sh
# .env
APP_CRON_HEARTBEAT_AUTO_ENABLED=true
```

```go
func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
registrar.RegisterParameter(melodycron.ParameterHeartbeatAutoEnabled, "%env(APP_CRON_HEARTBEAT_AUTO_ENABLED)%")
}
```

With the opt-in active and `--logs-dir=/var/log/cron`, the heartbeat path resolves to `/var/log/cron/heartbeat.crontab` — the heartbeat line then renders as `* * * * * <user> /bin/touch /var/log/cron/heartbeat.crontab` and is appended to every destination file by default (restrict it with `--heartbeat-destination`).

Precedence (highest → lowest):

1. `--heartbeat-path=<path>` (explicit flag wins, even when `melody.cron.heartbeat.enabled=true`).
2. `melody.cron.heartbeat_path` parameter (explicit container parameter).
3. `melody.cron.heartbeat.enabled` opt-in + non-empty `--logs-dir` → `<logs-dir>/heartbeat.crontab`.
4. Otherwise the heartbeat stays disabled (no parameter, no flag, no fallback).

`RegisterDefaultParameters` does **not** register `melody.cron.heartbeat.enabled` — the helper only registers safe, environment-agnostic defaults, and "emit a heartbeat" is an environment decision. Userland opts in explicitly.

`--heartbeat-command` (the custom heartbeat) is independent: it overrides `--heartbeat-path` (resolved or auto-derived) just like the documented heartbeat-path cascade, so setting `melody.cron.heartbeat.enabled=true` and `--heartbeat-command=/usr/bin/curl ...` together still produces the custom command line, not the auto-derived `touch`.

## Custom heartbeat command

`--heartbeat-path` produces the simple `/bin/touch <path>` line. For anything else (e.g. HTTP pings, custom commands), repeat `--heartbeat-command` once per argv token:

```sh
./app melody:cron:generate \
    --out=generated_conf/cron/crontab \
    --logs-dir=var/log/cron \
    --user=www-data \
    --heartbeat-command=/usr/bin/curl \
    --heartbeat-command=-fsS \
    --heartbeat-command=https://healthcheck.example.com/ping
```

Equivalent at the API level via `cron.Render`:

```go
content, err := cron.Render(entries, cron.RenderOptions{
HeartbeatUser:    "monitor",
HeartbeatCommand: []string{"/usr/bin/curl", "-fsS", "https://healthcheck.example.com/ping"},
})
```

When both `HeartbeatPath` and `HeartbeatCommand` are set, `HeartbeatCommand` wins.

## Heartbeat per-destination targeting

When schedules route entries to several destination files (via `EntryConfig.DestinationFile`), the heartbeat goes to **every** destination that is written. Restrict it to specific files with one or more `--heartbeat-destination=<value>` flags:

```sh
# heartbeat only in the default crontab
./app melody:cron:generate --heartbeat-path=/var/log/cron/heartbeat ... \
    --heartbeat-destination=default

# heartbeat in a specific custom destination
./app melody:cron:generate --heartbeat-path=/var/log/cron/heartbeat ... \
    --heartbeat-destination=billing-crontab
```

Accepted values: `default` (the `--out` destination), an absolute path, or a relative path matched against `dir(--out)`. Each value must match a destination actually being written — misspelled values error out instead of being silently ignored.

## Customizing the template

The generator dispatches rendering to a registered `Template` whose `Name()` matches `--template` (or, if unset, the `melody.cron.template` container parameter; default `"crontab"`). Two crontab dialects ship in-tree in all three bindings and are registered automatically:

* `crontab` — the `/etc/cron.d` system format, with the user column; requires a user per entry (`EntryConfig.User`, `--user`, or the `melody.cron.user` parameter).
* `crontab-no-user` — the user-less dialect for **busybox crond** (alpine images) and per-user `crontab` files, which reject the user column; the user parameter/flag is ignored entirely. One thing beside the column differs: this dialect alone refuses a day-field pair that busybox crond and vixie cron read as different schedules, with `ErrBusyboxDivergentDaySchedule`, so a configuration that renders under `crontab` can fail under `crontab-no-user`. Use this instead of post-processing the generated file with `sed` in the image build.

The v3 binding additionally ships a built-in `k8s` template (Kubernetes `CronJob` manifests). You can also plug in your own (Supervisor, custom YAML/INI, etc.) without forking the cron integration.

### `Template` interface

```go
type Template interface {
Name() string
Render(entries []Entry, options RenderOptions) (string, error)
}
```

`Render` is called once per output destination — `entries` are already expanded for multi-instance and have their `Binary`/`User` defaulted; `options` carries the resolved heartbeat configuration for this specific destination. The returned string is written atomically to disk by `melody:cron:generate` — each individual file is a temp-file-plus-rename, so `crond` never observes a truncated crontab. Any error (including `ValidateNoForbiddenCharacters` with your template's own forbidden-character list) aborts the generation immediately.

There is **no all-or-nothing guarantee across destinations.** `melody:cron:generate` walks the destinations in lexicographic order and renders and writes each one before moving to the next, so an error raised while rendering the third of four files leaves the first two already on disk and the last two absent. Per-file atomicity holds; a whole-run rollback does not. Validate a multi-destination configuration in a test, or treat a failed generation as "the output directory is now in an unknown state" and re-run once the error is fixed.

### Reconciling destinations across versions — `--prune`

A run writes the destinations the **current** configuration names, and by default touches nothing else. A destination an earlier version wrote and this one no longer names is therefore left exactly as it was: an entry you retired keeps running on every tick forever, and an entry you **moved** to another destination file runs from both — the double execution `melody:cron:run` refuses at construction, produced silently by the generator.

`--prune` closes that. It empties, in `dir(--out)`, the destinations this run did not produce, so the retired job stops. Three rules bound it, because emptying a file is not reversible:

* **Opt-in.** Without the flag the behaviour is exactly what it was. A deployment that manages the output directory itself — a release directory built fresh every time — needs nothing here.
* **Proof of ownership.** Only a file whose head carries the current template's ownership marker is touched. The built-in dialects render `cron.CrontabOwnershipMarker` in their header block; a file you or another tool put in the same directory carries no marker and is left alone. A template of your own opts in by implementing `cron.OwnedTemplate` and including the string it returns in **everything** `Render` produces, entries or none — a template that does not implement it is never pruned.
* **The output directory only.** The sweep reads `dir(--out)` and does not recurse. An entry that named an absolute `DestinationFile` outside that directory is written where you asked and is never swept: those files live where the operator put them.

Emptying means re-rendering the template with no entries, so the destination keeps its header — and with it its marker — and stays recognisable to the next run instead of becoming an unowned file the sweep would refuse to touch ever again. An empty configuration sweeps too: that is precisely the version in which every previously written destination is stale. The run stays a success either way, and the destinations it emptied are named on stdout and under `data.pruned` in the `--format=json` envelope, which is a list on every run.

```bash
melody:cron:generate --out /etc/cron.d/app --prune
```

### Built-in templates

`cron.BuiltinTemplates()` returns the list of templates shipped with the binding: the `crontab` template (`cron.TemplateNameCrontab == "crontab"`) and the user-less `crontab-no-user` variant (`cron.TemplateNameCrontabNoUser == "crontab-no-user"`) in all three bindings, plus the `k8s` template (`cron.TemplateNameK8s == "k8s"`) in the v3 binding only. `NewGenerateCommand` iterates this slice on construction, so you never register the built-ins by hand — they are always available even after you add your own.

### Built-in `k8s` template (v3 binding only)

`--template=k8s` renders the same `cron.Configuration` as a multi-document YAML stream — one `batch/v1` `CronJob` per scheduled command (`---`-separated) — suitable for `kubectl apply -f`. The same registry drives both the on-VPS crontab and the in-cluster CronJobs.

```bash
melody:cron:generate \
    --template=k8s \
    --image=registry.example.com/myapp:latest \
    --namespace=production \
    --restart-policy=OnFailure \
    --out=generated_conf/cron/cronjobs.yaml
```

Per `CronJob`:

* `metadata.name` is the command name sanitized to an RFC 1123 DNS label (lowercased; non-alphanumeric runs collapse to `-`; trimmed; capped at 52 octets so the generated job/pod suffixes stay within 63). `outbox:dispatch` → `outbox-dispatch`. Two commands that sanitize to the same name are a collision and fail generation rather than emitting CronJobs that would silently overwrite each other.
* `spec.schedule` is `Schedule.Expression()` (the same five-field expression the crontab template emits).
* The container runs the application image's entrypoint with `args: [<command-name>, …]`; the application enters CLI mode from those arguments (the same way the binary dispatches a command on the command line). A per-command `EntryConfig.Command` override instead replaces the entrypoint via the k8s `command:` field.
* `restartPolicy` defaults to `OnFailure`.

| Flag               | Parameter                        | Notes                                                                   |
|--------------------|----------------------------------|-------------------------------------------------------------------------|
| `--image`          | `melody.cron.k8s.image`          | **required** by the `k8s` template; the container image to run          |
| `--namespace`      | `melody.cron.k8s.namespace`      | optional; omitted from the manifest when empty                          |
| `--restart-policy` | `melody.cron.k8s.restart_policy` | optional; default `OnFailure`; only `OnFailure` or `Never` are accepted |

These parameters are **not** registered by `RegisterDefaultParameters` (the crontab template needs none of them); set them via the flags or register them yourself. The heartbeat options (`--heartbeat-path` / `--heartbeat-command`) are crontab-only and are ignored by the `k8s` template (which prints a warning when they are set, so the dropped liveness entry is not silent) — model cluster liveness with a dedicated scheduled command instead.

### Registering a custom template

```go
import (
melodycron "github.com/precision-soft/melody/integrations/cron/v3"
)

type KubernetesCronjobTemplate struct {
Namespace string
Image     string
}

func (instance *KubernetesCronjobTemplate) Name() string {
return "k8s_cronjob"
}

func (instance *KubernetesCronjobTemplate) Render(entries []melodycron.Entry, options melodycron.RenderOptions) (string, error) {
forbidden := []melodycron.ForbiddenCharacter{
{Char: '\t', Reason: "tabs break yaml indentation"},
}
for _, entry := range entries {
if validationErr := melodycron.ValidateNoForbiddenCharacters(entry.Command, forbidden, "k8s entry "+entry.Name); nil != validationErr {
return "", validationErr
}
}
// ... build the YAML from entries + instance.Namespace + instance.Image ...
return yamlContent, nil
}

var _ melodycron.Template = (*KubernetesCronjobTemplate)(nil)
```

Hand the instance to `GenerateCommand.RegisterTemplate` before the kernel runs `melody:cron:generate`:

```go
func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
commands := []melodyclicontract.Command{
command.NewAppInfoCommand(),
command.NewProductListCommand(),
}

cronConfiguration := melodycron.NewConfiguration().
Schedule(melodycron.CommandName(command.NewProductListCommand), &melodycron.EntryConfig{
Schedule: &melodycron.Schedule{Minute: "0", Hour: "3"},
})

generateCommand := melodycron.NewGenerateCommand(cronConfiguration)
generateCommand.RegisterTemplate(&KubernetesCronjobTemplate{
Namespace: "production",
Image:     "myapp:latest",
})

return append(commands, generateCommand)
}
```

### Selecting the active template

Three sources, in priority order:

1. `--template=<name>` on the CLI (explicit, wins over everything).
2. `melody.cron.template` container parameter (set by `RegisterDefaultParameters` to `"crontab"`, override via env-driven parameters or your own `RegisterParameter` call).
3. Fallback: `"crontab"` (`cron.TemplateNameCrontab`).

If the resolved name has no template registered, `melody:cron:generate` errors with a message listing the names that **are** registered, so a typo in `melody.cron.template = "k8s-cronjob"` (dash instead of underscore) surfaces immediately.

### Template-specific configuration

Each template owns its config shape via struct fields (`Namespace`, `Image` in the example above). Userland reads whatever it needs from melody parameters / env / config files at bootstrap and injects the values when constructing the template instance. The cron integration does **not** mediate template-specific config — it only resolves the active template name. This keeps each template self-contained and avoids leaking unrelated knobs into `cron`'s parameter namespace.

## Usage

### 1. Write a plain Melody CLI command

```go
package command

import (
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type ProductListCommand struct{}

func NewProductListCommand() *ProductListCommand {
    return &ProductListCommand{}
}

func (instance *ProductListCommand) Name() string {
    return "product:list"
}

func (instance *ProductListCommand) Description() string {
    return "prints products in a table"
}

func (instance *ProductListCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *ProductListCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
    return nil
}

var _ melodyclicontract.Command = (*ProductListCommand)(nil)
```

The command is a plain Melody CLI command — there is no cron-specific interface to implement. Scheduling lives in the configuration registry below.

### 2. Build the `cron.Configuration` registry and wire the generator

```go
import (
melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
melodycron "github.com/precision-soft/melody/integrations/cron/v3"
melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
commands := []melodyclicontract.Command{
command.NewAppInfoCommand(),
command.NewProductListCommand(),
}

cronConfiguration := melodycron.NewConfiguration().
Schedule(melodycron.CommandName(command.NewProductListCommand), &melodycron.EntryConfig{
Schedule: &melodycron.Schedule{Minute: "0", Hour: "3"},
}).
Schedule(melodycron.CommandName(command.NewAppInfoCommand), &melodycron.EntryConfig{
Schedule: &melodycron.Schedule{Minute: "0", Hour: "12"},
})

return append(commands, melodycron.NewGenerateCommand(cronConfiguration))
}
```

`cron.CommandName(NewProductListCommand)` instantiates the constructor once and returns `command.Name()`, so the schedule references the command by constructor instead of a hardcoded string. The `Configuration` preserves insertion order — entries appear in the crontab in registration order.

### Full reference module

A copy-pasteable end-to-end module wiring — scheduled command, custom `KubernetesCronjobTemplate`, `RegisterParameters` + `RegisterCliCommands` with template registration — lives in [`.example/module.go`](./.example/module.go) (and the matching files under [`v2/.example/`](./v2/.example/module.go) / [`v3/.example/`](./v3/.example/module.go) for the other bindings). The directory is dot-prefixed so `go build ./...` and `go test ./...` skip it; build it explicitly with `go build ./.example/`.

### 3. Generate the crontab file

```sh
./app melody:cron:generate \
    --out=generated_conf/cron/crontab \
    --logs-dir=var/log/cron \
    --user=www-data \
    --heartbeat-path=var/log/cron/heartbeat.crontab
```

Producing:

```
#############################################################################
#
# GENERATED FILE
# DO NOT EDIT LOCALLY
#
# owned by melody:cron:generate
#############################################################################
# Example of job definition:
# .---------------- minute (0 - 59)
# |  .------------- hour (0 - 23)
# |  |  .---------- day of month (1 - 31)
# |  |  |  .------- month (1 - 12) OR jan,feb,mar,apr ...
# |  |  |  |  .---- day of week (0 - 6) (Sunday=0 or 7) OR sun,mon,tue,wed,thu,fri,sat
# |  |  |  |  |
# *  *  *  *  * user-name command to be executed
#############################################################################
0 3 * * * www-data /abs/path/app product:list >> '/abs/path/var/log/cron/product-list.log' 2>&1

* * * * * www-data /bin/touch /abs/path/var/log/cron/heartbeat.crontab
#############################################################################
```

When no command declares a schedule and `--heartbeat-path` (after the cascade) is empty, the command prints `nothing to write` and exits without creating the output file.

## Footguns & caveats

- Outside `--template` (crontab dialect) and `--binary` (the running executable), the cron command has **no hardcoded fallback values**. Without either a CLI flag or a container parameter, `melody:cron:generate` errors out at run time for the remaining flags with a message naming the missing flag/parameter. Use `RegisterDefaultParameters` for sensible defaults, or wire each parameter explicitly.
- `Render(...)` does **not** silently default empty users. Every `Entry` must carry a non-empty `User`, and a non-empty `Binary` **or** non-empty `Command`. `HeartbeatPath` and `HeartbeatCommand` both require a `HeartbeatUser`. `melody:cron:generate` resolves the user via the cascade before calling `Render`, but anyone calling `Render` directly is responsible for the same.
- `Schedule.Expression()` auto-fills empty fields with `*` wildcards without mutating the receiver. Use `Schedule.Defaults()` if you also want the struct fields populated in place (nil-safe).
- `Schedule.Defaults()` **mutates the receiver** (returning the same pointer with empty fields replaced by `*`); the name is "Defaults" in the sense of "apply defaults", not "return a copy with defaults". If you need an unchanged original, copy the struct before the call.
- Entries appear in registration order, i.e. the order of `Configuration.Schedule(...)` calls. Re-ordering the builder calls reshuffles the generated crontab.
- Destination files are written in lexicographic order; each destination file is written atomically (temp file + rename) so `crond` never observes a truncated crontab even if the generator is killed mid-write.
- The heartbeat line is appended to every destination file that gets written, unless restricted with `--heartbeat-destination` (see [Heartbeat per-destination targeting](#heartbeat-per-destination-targeting)).
- The per-command log file name defaults to `<sanitized-command-name>.log` where `:` and `/` are replaced by `-`. Override per command with `EntryConfig.LogFileName`, opt out of `:` sanitization with `EntryConfig.LogFileNameRaw = true`, or disable logging entirely with `EntryConfig.LogDisabled = true`.
- `EntryConfig.LogFileName` is joined with `--logs-dir` and rejected if the result escapes that directory (e.g. `"../escape.log"`), mirroring the `EntryConfig.DestinationFile` guard. Use a file name (or relative path) that stays inside the configured logs dir. A relative path that names a subdirectory (`"nightly/report.log"`) is honored, and the subdirectory is created at generate time — under system cron the shell aborts the whole command when the `>>` redirection cannot create its file, so a missing log directory would mean the job silently never runs.
- **Timezone is not coupled between the runner and the generated crontab.** `melody:cron:run` evaluates every schedule against the process's local time (`TZ` of the process, `time.Local`), while the generated crontab runs under the cron daemon's own zone — most container images default to UTC — and no `TZ=` line is emitted into the file. The same `Configuration` therefore fires at different absolute instants when the two zones differ; align the process's `TZ` with the daemon's (or run both in UTC) before treating the runner and the manifests as interchangeable.
- **The user-less dialect refuses day-field pairs busybox reads differently.** busybox crond classifies a day field by its expanded values — a field admitting every value (`0-6`, `sun-sat`, `1-31`) counts as unused and the other field alone governs — while vixie crond and the in-process runner read the spelling's first character. A pair the two daemons would run as two different schedules (`DayOfMonth: "16"` with `DayOfWeek: "0-6"`, or a stepped wildcard beside a restricted sibling) fails generation with `ErrBusyboxDivergentDaySchedule` and a message naming the rewrite; restrict a single day field and leave the other as the plain `*`. The `/etc/cron.d` dialect keeps rendering such pairs, since vixie reads them exactly as the runner does.
- Multi-instance log file names preserve compound extensions: `EntryConfig.LogFileName = "archive.tar.gz"` with `Instances = 2` yields `archive-1.tar.gz` and `archive-2.tar.gz` (not `archive.tar-1.gz`).
- `EntryConfig.Instances` is intended for the default `<binary> <command-name>` shape — it appends `--max-instances` / `--instance-index` flags to your binary's arg list. When you set `EntryConfig.Command` with custom argv, those flags are **not** injected (the generator still emits N entries, each with the same argv and a per-instance log file); inject the flags yourself or build N distinct commands. Values `< 1` (zero or negative) are normalized to `1`.
- `EntryConfig.DestinationFile` accepts absolute paths verbatim — the `dir(--out)` escape check applies only to relative values. An absolute `DestinationFile` of e.g. `/etc/cron.d/billing` is honored as-is, so the generator can write outside the default output directory when a command genuinely needs it. Relative paths are joined with `dir(--out)` and rejected if they escape that directory (e.g. `"../escape"`).
- Generated crontab files are written with mode `0644` and their parent directories are created with mode `0755`. Both modes are intentionally non-configurable; if your target requires stricter permissions, run `chmod` from your deploy script after `melody:cron:generate` returns.
- The resolved `--logs-dir` (or `melody.cron.logs_dir`) is auto-created with mode `0755` at generate time via `os.MkdirAll` — you do not need a separate `mkdir -p` step before `crond` runs the entries. The mkdir is skipped only when `logsDir` is empty (heartbeat-only or fully `LogDisabled` schedules); other failures (e.g. parent path is a file, permission denied) error out with `cron: could not create the logs directory`. The created directory's owner/group default to the user running `melody:cron:generate` — if your cron jobs run as a different system user (`apache`, `www-data`, …), run `chown`/`chgrp` from your deploy script so the entries can write their log files.
- A blank `Schedule` value (every field empty) renders as `* * * * *`. That is intentional but rarely the right call — always set at least `Minute`.
- The renderer applies POSIX shell quoting **per token** for `EntryConfig.Command`, `entry.Binary`, `entry.Args`, `RenderOptions.HeartbeatCommand`, `LogPath`, and `HeartbeatPath` — tokens containing spaces, quotes, or other shell metacharacters are single-quoted (with embedded `'` escaped as `'\''`). Tokens with only safe characters are emitted unchanged, except `LogPath`, which is always single-quoted. **Never** pre-quote a token yourself or wrap a whole `binary arg1 arg2` string in quotes; the per-token quoting is enough, and pre-quoting turns the whole sequence into a single filename to `crond`. `%`, `\n`, and `\r` are rejected in any token because they have special meaning to the crontab daemon itself (line continuation / line termination) regardless of quoting; remove them at the source.
- `User` fields (`EntryConfig.User`, the resolved `--user` cascade, `RenderOptions.HeartbeatUser`) are validated against any whitespace — Unicode spaces included, leading and trailing alike — before rendering. A username with whitespace would split the crontab line apart; the generator rejects it with `cron: ... contains whitespace; user fields must be single tokens`. Usernames already come from trusted application code in practice — this is a defense-in-depth check.
- `melody:cron:generate --format=json` renders one envelope on **every** outcome, the failures included: the failure travels as `error.code = "cron.generateFailed"` with the failure's own text as `error.cause.message`, its context under `error.details` and the whole chain beneath it under `error.cause.details.chain`, and `data.writes` still names the destinations already written — the generator writes one destination at a time with no rollback, so a failure on the third of four leaves two files on disk. In text mode a failure is reported by the cli entry point as before.
- `melody:cron:run` writes one envelope per dispatched minute: `data` carries the minute, the number of configured entries and each run with its command, schedule, arguments, `runId`, duration and verdict, and an aggregated failure travels as `error.code = "cron.runFailed"` naming the failed commands under `error.details.failed`. `--once` writes exactly one document and exits, which is the form a deploy step or a system crontab entry invokes; the scheduler loop writes one document per minute that dispatched something and stays silent on the idle minutes unless `--report-idle` asks for them, so a long-lived scheduler under `--format=json` is a stream of one closed document per line rather than a summary that would only arrive when the process dies — one line per document is what the json format writes, which is what makes `while read line` and a log collector's json stage work on it. `--format=json-pretty` prints the same document indented, for reading by hand rather than by a consumer.
- Whichever mode it runs in, the runner files one record at the head of the run naming how many entries it drives and the schedule of each — at **warning** when the number is zero, because a scheduler built over a configuration emptied by a refactor, or whose entries an environment gate filtered away, would otherwise run forever, exit successfully and be indistinguishable from a healthy one.
- Under the in-process runner (`melody:cron:run`), a scheduled command's own output goes to the **journal**, not to the process stdout: due entries run concurrently and would otherwise interleave their reports on one stream. Each run that printed anything files one record — `cron: scheduled command output` — carrying `commandName`, `cronRunId` and the output itself, at info when the run succeeded and warning when it did not. What one run may contribute is capped at 64 KiB, with the cut named in `commandOutputDroppedBytes`.
- The `EntryConfig.LogFileName` / `EntryConfig.DestinationFile` containment check is **lexical**: it rejects relative paths whose joined result has a `..` segment escaping the parent (`--logs-dir` or `dir(--out)`). Symlinks are not resolved — the threat model assumes these values come from trusted application code, so the only case guarded against is a literal `..` escape attempt.

## Package surface

The v1, v2 and v3 bindings share a common core surface, and **neither side is a superset of the other**. v3 additionally ships the built-in `k8s` template with its errors and parameters, plus a `Commands` helper. v1 and v2 additionally declare `OwnedTemplate`, `UserColumnTemplate`, `RendersUserColumn`, `CrontabOwnershipMarker` and `ErrBusyboxDivergentDaySchedule`, none of which v3 has. Do not treat the surface as identical across bindings: code written against either side's own identifiers does not compile on the other.

Common to all three, from any of `github.com/precision-soft/melody/integrations/cron`, `.../cron/v2`, or `.../cron/v3`:

* Types: `Schedule`, `EntryConfig`, `Configuration`, `ScheduledCommand`, `Entry`, `RenderOptions`, `GenerateCommand`, `RunnerCommand`, `RunnerDialect`, `Module`, `ModuleConfig`, `Template`, `CrontabTemplate`, `ParameterRegistrar`, `ForbiddenCharacter`.
* Constructors / helpers: `NewConfiguration`, `NewGenerateCommand`, `NewRunnerCommand`, `NewModule`, `CommandName`, `Render`, `BuiltinTemplates`, `ValidateNoForbiddenCharacters`, `RegisterDefaultParameters`.
* Parameter-name constants: `ParameterUser`, `ParameterLogsDir`, `ParameterBinary`, `ParameterDestinationFile`, `ParameterHeartbeatPath`, `ParameterHeartbeatAutoEnabled`, `ParameterTemplate`.
* Template-name constants: `TemplateNameCrontab`, `TemplateNameCrontabNoUser`.
* Globals: `CrontabForbiddenCharacters`.
* Sentinel errors ([`./errors.go`](./errors.go)) for `errors.Is` matching: `ErrNoOutputPath`, `ErrNoLogsDir`, `ErrEntryEmptyUser`, `ErrEntryEmptyCommand`, `ErrDestinationEscape`, `ErrFieldContainsWhitespace`, `ErrSteppedSingleValue`, `ErrInvalidSchedule`, `ErrForbiddenCharacter`, `ErrTemplateNotFound`, `ErrHeartbeatUserMissing`, `ErrHeartbeatDestinationUnmatched`, `ErrHeartbeatDestinationDefaultMissing`, `ErrUnknownScheduledCommand`, `ErrDuplicateRunnerCommand`, `ErrSharedRunnerCommandFlags`, `ErrUnsupportedRunnerEntry`, `ErrUnknownRunnerDialect`, `ErrCommandTimeout`.

`ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars` still exist in all three bindings as **deprecated** aliases of `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters` ([`./validation.go`](./validation.go)); use the spelled-out names in new code.

The **v3 binding only** additionally exposes:

* The built-in `k8s` template: `K8sTemplate`, `TemplateNameK8s`.
* Its parameter-name constants: `ParameterImage`, `ParameterNamespace`, `ParameterRestartPolicy`.
* Its sentinel errors: `ErrK8sImageMissing`, `ErrK8sInvalidRestartPolicy`, `ErrK8sInvalidName`, `ErrK8sDuplicateName`.
* `Commands(configuration *Configuration) []clicontract.Command` ([`v3/command.go`](./v3/command.go)) — the integration's commands as a slice, for applications that wire `RegisterCliCommands` by hand instead of registering the module.
