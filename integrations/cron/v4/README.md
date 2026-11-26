# Cron generator — Melody v4 binding

Melody v4 binding for the [`precision-soft/melody/integrations/cron`](..) crontab generator. See the [umbrella README](../README.md) for the full design, configuration parameters, cascade rules, per-command overrides, custom heartbeat command, [template customization](../README.md#customizing-the-template) (plug your own `Template` for Kubernetes / Supervisor / ...) and footguns.

## Install

```bash
go get github.com/precision-soft/melody/integrations/cron/v4@latest
```

## Import paths

```go
import (
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron/v4"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)
```

## Quick wiring

See the [umbrella README](../README.md#configuration-parameters) for the full wiring walk-through (`cron.Configuration` registry, parameter cascade, per-command overrides). The same examples apply with the import paths above, except three things: on this major the module declares the keys that feed the parameters (see [Keys](#keys)), and `RegisterDefaultParameters` and `WithDefaultParameters` do not exist; the module is the one door that builds the generator, `NewGenerateCommand` and `cron.EnvironmentKeys` being unexported and `GenerateCommand.RegisterTemplate` and the `Commands(configuration)` helper removed, since a generator built by hand would run with keys nobody declared; and the template contract carries the ownership line and the user column (see [Templates](#templates)).

One `RegisterModule` call contributes the crontab-generation command and declares the module's keys; `ModuleConfig.Configuration` is a factory evaluated at command-registration time, when the kernel exists:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return configuration
    },
    KeyDefaults: melodycron.KeyDefaults{User: "www-data"},
}))
```

## Running on several replicas

The in-process runner (`melody:cron:run`) runs each tick of an entry on one replica. Before it runs a due entry it acquires a marker named `<KeyPrefix><command name>:<digest>:<unix minute>`, the digest covering the command name, the schedule and the arguments, and keeps it past the run for the hold, so a replica evaluating that minute later lists the entry under `skipped` in the minute's document, an empty list when nothing was skipped. A minute of skips only is as silent as an idle one unless `--report-idle` is passed. It is at most one attempt per tick: a tick whose winner failed, timed out or crashed is spent and is not retried elsewhere.

Three wirings: nothing, and an application that registers `service.lock.locker` is deduplicated through it, the locker looked up once per `Run` in the run's scope; `RunnerConfig.TickDedup.Locker` names one; `RunnerConfig.TickDedup.Disabled` turns the dedup off. With neither a named nor a registered locker the runner deduplicates nothing and `Run` writes one warning.

| Door | Default |
|---|---|
| `RunnerConfig.TickDedup.Disabled` | `false`, on |
| `EntryConfig.TickDedup.Disabled` | `false`: the entry follows the runner; `true` runs it on every replica, the door for a job per-replica by nature |
| `RunnerConfig.TickDedup.Locker` | the registered `service.lock.locker`; a typed nil refused |
| `RunnerConfig.TickDedup.KeyPrefix` | `<namespace>:cron:tick:`, `melody:cron:tick:` under the default namespace; whitespace or a control character refused |
| `RunnerConfig.TickDedup.Window`, `EntryConfig.TickDedup.Window` | 5m, the entry's the runner's when left out; at least 1s, unlimited refused |
| `RunnerConfig.TickDedup.FailureMode`, `EntryConfig.TickDedup.FailureMode` | `cron.TickDedupFailureSkip`; `cron.TickDedupFailureRun` runs through an outage |
| `RunnerConfig.TickDedup.CallTimeout` | 5s per acquire and per release; at least 1ms, unlimited refused |

Every value is Go-side only; the runner has no env key.

The hold is `Window` for an entry with a wildcard minute or hour, and `Window` plus `RunnerConfig.ClockJumpResetThreshold` for one that pins a minute and an hour, since such an entry is also evaluated in catch-up up to the threshold late. Size `Window` above the worst clock skew plus wake latency between two replicas; the live markers per entry are the ticks inside one hold, five for an every-minute entry at the default. The runner releases each won marker once its hold has elapsed, and nothing at shutdown or after `--once`, so a rolling deploy does not reopen the ticks it won.

The locker: a lease locker, the rueidis one, keeps the marker past its writer; on the rueidis locker the key reads `<namespace>:lock:<namespace>:cron:tick:…`, the lock prefix and the tick prefix both carrying the namespace, which is redundant and harmless. A session locker, the mysql and pgsql ones, pins one connection per held tick and loses the marker with its process, so a replica stopped inside the hold reopens the ticks it won. An in-memory locker deduplicates nothing across processes.

An outage: an acquire that errors, a `CallTimeout`, a panic of the locker, a `CreateLock` refusal or a registered locker that cannot be resolved leaves the tick not run under `TickDedupFailureSkip`, reported as that run's failure under `ErrTickDedupUnavailable`, so `--once` exits non-zero; `TickDedupFailureRun` runs it with one warning naming the key. An acquire the shutdown ended is a cancellation, not a failure.

Clock steps: a replica re-runs a tick it won itself exactly as a single host would, the vixie re-run after a backward step included; a tick another replica holds is skipped. A rolling deploy that changes an entry's schedule or arguments changes that entry's key until the deploy ends.

Overlap is a separate concern: `lock.NewExclusiveCommand` serialises overlapping runs of one command. Used together, the runner wins the tick and the wrapper takes its own run-long lock inside the run; a tick won while the previous run still holds the wrapper's lock is skipped by the wrapper and is spent. The generated crontab and CronJob manifests are not covered: several hosts carrying one manifest need the external scheduler's own single-run mechanism, or the runner.

## Keys

The module declares ten `.env` keys through the framework's [module keys](../../../v4/.documentation/package/CONFIG.md#module-keys): each key feeds its parameter through `%env(KEY)%`, the `.env` line wins, then the `ModuleConfig.KeyDefaults` field, then the module's default, and a key with a rule is judged at boot in every mode the module is registered in, refused naming the key and the `cron.KeyDefaults.<Field>`. A cli flag still wins over the parameter at generation.

| Key                              | Parameter                        | Module default                                           | Rule                   |
|----------------------------------|----------------------------------|----------------------------------------------------------|------------------------|
| `MELODY_CRON_USER`               | `melody.cron.user`               | `""`                                                     | —                      |
| `MELODY_CRON_LOGS_DIRECTORY`     | `melody.cron.logs_directory`     | `%kernel.logs_directory%/cron`                           | —                      |
| `MELODY_CRON_BINARY`             | `melody.cron.binary`             | `""` (the running executable)                            | —                      |
| `MELODY_CRON_DESTINATION_FILE`   | `melody.cron.destination_file`   | `%kernel.project_directory%/generated_conf/cron/crontab` | —                      |
| `MELODY_CRON_HEARTBEAT_PATH`     | `melody.cron.heartbeat_path`     | `""`                                                     | —                      |
| `MELODY_CRON_HEARTBEAT_ENABLED`  | `melody.cron.heartbeat.enabled`  | `""` (off)                                               | a boolean              |
| `MELODY_CRON_TEMPLATE`           | `melody.cron.template`           | `crontab`                                                | —                      |
| `MELODY_CRON_K8S_IMAGE`          | `melody.cron.k8s.image`          | `""`                                                     | —                      |
| `MELODY_CRON_K8S_NAMESPACE`      | `melody.cron.k8s.namespace`      | `""`                                                     | an RFC 1123 label      |
| `MELODY_CRON_K8S_RESTART_POLICY` | `melody.cron.k8s.restart_policy` | `""` (`OnFailure`)                                       | `OnFailure` or `Never` |

The key `MELODY_CRON_LOGS_DIRECTORY` retires the spelling `MELODY_CRON_LOGS_DIR`, which only the unreleased fourth major carried, through `EnvironmentKey.Retired`: a `.env` line still setting it, empty or beside the new key, refuses the boot with `the environment key MELODY_CRON_LOGS_DIR is retired: rename it to MELODY_CRON_LOGS_DIRECTORY`, in the framework's one refusal of the [retired keys](../../../v4/.documentation/package/CONFIG.md#retired-keys). A parameter the application still registers as `melody.cron.logs_dir`, the third major's name, refuses the boot when the module registers its commands, after the resolve, in every process shape, with `the parameter melody.cron.logs_dir is retired: rename it to melody.cron.logs_directory`; a value still naming `%kernel.logs_dir%` is refused first by the resolve as an undefined parameter, so that migration takes two boots. Neither refusal carries the value, and neither has a door.

An empty `.env` line takes the default on the three keys with a rule and is the empty value on the seven free-text ones: `MELODY_CRON_DESTINATION_FILE=` and `MELODY_CRON_LOGS_DIRECTORY=` leave the generator with no destination and no logs directory, so a run without `--out` is refused and a run without `--logs-directory` refuses an entry that redirects its output.

The generator creates the logs directory, once the entries are known and before anything is rendered, since under system cron a shell whose `>>` cannot create its file aborts the command. With a template that runs every entry as a user (`crontab`), a generator running as root refuses, before it creates or writes anything, a logs directory the entries' users could not use: a missing one, which root would create as its own, for a user that writes its log file, or the heartbeat, directly in it; an existing one that user cannot write and enter, or, for a user logging in a subdirectory, cannot enter. The refusal, `ErrLogsDirectoryNotWritable` in its chain, names the directory and the users in its message and the commands in its context; create the directory beforehand with an owner or a mode they can write, or give those entries log file names in directories of their own, which the generator creates and hands to their users as before. Not judged: a user the host does not know (a build host generating for others), uid 0 under any name, a user whose groups cannot be read, a directory carrying a POSIX access control list (its mode bits do not tell), and a direct writer whose log file exists already and is writable for it, the shape a `logrotate` `create` line leaves. An existing subdirectory is not judged either.

A parameter of these names registered by hand is refused at boot.

The `Configuration` factory reads the kernel when the schedule depends on it (resolved parameters, the manager registry, schedules referencing container services); a factory answering nil is refused at registration naming `ModuleConfig.Configuration`, and with no factory the module registers no command, `Runner.Commands` or `Templates` without one refused:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return melodycron.NewConfiguration().Schedule(/* … reads kernelInstance.Config() … */)
    },
}))
```

`ModuleConfig.Runner` adds the in-process `melody:cron:run` scheduler beside the generator; its `Commands` are the registered commands the configuration schedules by name. An entry that names no `Timeout` has its context cancelled after the runner's, one hour unless `RunnerConfig.Timeout` names another; an entry that should run with no deadline writes `bound.UnlimitedDuration()`:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return melodycron.NewConfiguration().
            Schedule("app:report:nightly", melodycron.EntryConfig{
                Schedule: &melodycron.Schedule{Minute: "0", Hour: "2"},
                Timeout:  bound.Of(30 * time.Minute),
            })
    },
    Runner: melodycron.RunnerConfig{
        Commands: []melodyclicontract.Command{reportCommand},
    },
}))
```

`RunnerConfig` also carries `Dialect`, `OutputCaptureLimit` (64 KiB), `ClockJumpResetThreshold` (3h, at least 2m, whole minutes) and `ShutdownTimeout` (5m); each is its default when left out, and a value below its minimum or an unlimited one where the field refuses it panics at boot naming the field.

`NewModule` is available for the v1, v2, v3 and v4 bindings.

## Templates

On this major the capabilities are methods of the contract, so no template is probed:

```go
type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
    OwnershipMarker(applicationName string) string
    RendersUserColumn() bool
}
```

`ModuleConfig.Templates` installs templates after the builtin dialects, selectable by `--template`; one named as a builtin replaces it, and a nil entry is refused naming `ModuleConfig.Templates[<index>]`. The generator hands every template the application's cli name in `RenderOptions.ApplicationName`, empty when the cli configuration carries none, and asks `OwnershipMarker(applicationName)` for the line `--prune` recognises among the first ten lines of a destination: the builtins answer `cron.CrontabOwnershipMarker + " for " + applicationName`, the bare prefix for an empty name, so a template embedding a builtin carries the application's line and its sweep works. A name carrying a control character, a line break above all, is refused with `ErrInvalidApplicationName` before anything renders, by the generator and by the builtins' `Render`, `cron.Render` included, since it would break the one-line marker into an active line; `ValidateApplicationName` is the same check for a custom dialect's `Render`. A template answering a blank marker is written and never pruned; one answering a marker that does not end in ` for ` and the cli name is written and its sweep refused, naming the line. `RendersUserColumn` tells the generator, before rendering, whether a heartbeat line needs a user.

## Module dependencies

This module requires:

- `github.com/precision-soft/melody/v4` ≥ v4.0.0 (the minimum this module's [`go.mod`](./go.mod) requires)

Everything else is stdlib — the flag-parsing engine reaches this binding through the framework's cli aliases, as an indirect requirement; no source of this module names it. The package surface is shared across all three bindings — the ownership and user-column surface included (`CrontabOwnershipMarker`, `ErrBusyboxDivergentDaySchedule`; on this binding `OwnershipMarker` and `RendersUserColumn` are methods of `Template`) — apart from three things: this binding additionally ships the built-in `k8s` template (`K8sTemplate`, `TemplateNameK8s`, its `ParameterImage`/`ParameterNamespace`/`ParameterRestartPolicy` constants and its `ErrK8sImageMissing`/`ErrK8sInvalidRestartPolicy`/`ErrK8sInvalidName`/`ErrK8sDuplicateName` errors) and the validators and shell quoting a custom crontab-like dialect renders through (`ValidateScheduleFields`, `ValidateUserField`, `ValidateApplicationName`, `JoinShellTokens`, `ShellQuoteIfNeeded`), while the `Commands(configuration)` helper the third binding ships is removed here; it alone gives the in-process runner a zone of its own
(`Configuration.InTimezone`, `Configuration.TimezoneName`, the runner's `--timezone` flag and `ErrUnknownTimezone`), while the generated manifests stay the external scheduler's to place in time; the deprecated abbreviated validation aliases the earlier bindings keep (`ForbiddenChar`, `CrontabForbiddenChars`, `ValidateNoForbiddenChars`) are removed from this one. See [Package surface](../README.md#package-surface) in the umbrella README for the lists, then the [umbrella README](../README.md) for the design details.
