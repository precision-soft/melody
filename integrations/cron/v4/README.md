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

See the [umbrella README](../README.md#configuration-parameters) for the full wiring walk-through (`cron.Configuration` registry, parameter cascade, per-command overrides). The same examples apply with the import paths above, except three things: on this major the module declares the keys that feed the parameters (see [Keys](#keys)), and `RegisterDefaultParameters` and `WithDefaultParameters` do not exist; the module is the door that builds the generator, `NewGenerateCommand`, `GenerateCommand.RegisterTemplate` and `cron.EnvironmentKeys` being unexported; and the template contract carries the ownership line and the user column (see [Templates](#templates)).

One `RegisterModule` call contributes the crontab-generation command and declares the module's keys; `ModuleConfig.Configuration` is a factory evaluated at command-registration time, when the kernel exists:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return configuration
    },
    KeyDefaults: melodycron.KeyDefaults{User: "www-data"},
}))
```

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

`ModuleConfig.Templates` installs templates after the builtin dialects, selectable by `--template`; one named as a builtin replaces it, and a nil entry is refused naming `ModuleConfig.Templates[<index>]`. The generator hands every template the application's cli name in `RenderOptions.ApplicationName`, empty when the cli configuration carries none, and asks `OwnershipMarker(applicationName)` for the line `--prune` recognises among the first ten lines of a destination: the builtins answer `cron.CrontabOwnershipMarker + " for " + applicationName`, the bare prefix for an empty name, so a template embedding a builtin carries the application's line and its sweep works. A template answering a blank marker is written and never pruned; one answering a marker that does not end in ` for ` and the cli name is written and its sweep refused, naming the line. `RendersUserColumn` tells the generator, before rendering, whether a heartbeat line needs a user.

## Module dependencies

This module requires:

- `github.com/precision-soft/melody/v4` ≥ v4.0.0 (the minimum this module's [`go.mod`](./go.mod) requires)

Everything else is stdlib — the flag-parsing engine reaches this binding through the framework's cli aliases, as an indirect requirement; no source of this module names it. The package surface is shared across all three bindings — the ownership and user-column surface included (`CrontabOwnershipMarker`, `ErrBusyboxDivergentDaySchedule`; on this binding `OwnershipMarker` and `RendersUserColumn` are methods of `Template`) — apart from three things: this binding additionally ships the built-in `k8s` template (`K8sTemplate`, `TemplateNameK8s`, its `ParameterImage`/`ParameterNamespace`/`ParameterRestartPolicy` constants and its `ErrK8sImageMissing`/`ErrK8sInvalidRestartPolicy`/`ErrK8sInvalidName`/`ErrK8sDuplicateName` errors) and the `Commands(configuration)` helper, and the validators and shell quoting a custom crontab-like dialect renders through (`ValidateScheduleFields`, `ValidateUserField`, `JoinShellTokens`, `ShellQuoteIfNeeded`); it alone gives the in-process runner a zone of its own
(`Configuration.InTimezone`, `Configuration.TimezoneName`, the runner's `--timezone` flag and `ErrUnknownTimezone`), while the generated manifests stay the external scheduler's to place in time; the deprecated abbreviated validation aliases the earlier bindings keep (`ForbiddenChar`, `CrontabForbiddenChars`, `ValidateNoForbiddenChars`) are removed from this one. See [Package surface](../README.md#package-surface) in the umbrella README for the lists, then the [umbrella README](../README.md) for the design details.
