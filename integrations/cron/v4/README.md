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

See the [umbrella README](../README.md#configuration-parameters) for the full wiring walk-through (`cron.Configuration` registry, `NewGenerateCommand(configuration)`, parameter cascade, per-command overrides). The same examples apply with the import paths above, except the parameters: on this major the module declares the keys that feed them (see [Keys](#keys)), and `RegisterDefaultParameters` and `WithDefaultParameters` do not exist.

Or bundle it as a self-registering application module — one `RegisterModule` call contributes the crontab-generation command and declares the module's keys:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: configuration,
    KeyDefaults:   melodycron.KeyDefaults{User: "www-data"},
}))
```

## Keys

The module declares ten `.env` keys through the framework's [module keys](../../../v4/.documentation/package/CONFIG.md#module-keys): each key and its parameter are one parameter, the `.env` line wins, then the `ModuleConfig.KeyDefaults` field, then the module's default, and a key with a rule is judged at boot in every mode the module is registered in, refused naming the key and the `cron.KeyDefaults.<Field>`. A cli flag still wins over the parameter at generation.

| Key                              | Parameter                        | Module default                                     | Rule                   |
|----------------------------------|----------------------------------|----------------------------------------------------|------------------------|
| `MELODY_CRON_USER`               | `melody.cron.user`               | `""`                                               | —                      |
| `MELODY_CRON_LOGS_DIRECTORY`     | `melody.cron.logs_directory`     | `%kernel.logs_dir%/cron`                           | —                      |
| `MELODY_CRON_BINARY`             | `melody.cron.binary`             | `""` (the running executable)                      | —                      |
| `MELODY_CRON_DESTINATION_FILE`   | `melody.cron.destination_file`   | `%kernel.project_dir%/generated_conf/cron/crontab` | —                      |
| `MELODY_CRON_HEARTBEAT_PATH`     | `melody.cron.heartbeat_path`     | `""`                                               | —                      |
| `MELODY_CRON_HEARTBEAT_ENABLED`  | `melody.cron.heartbeat.enabled`  | `""` (off)                                         | a boolean              |
| `MELODY_CRON_TEMPLATE`           | `melody.cron.template`           | `crontab`                                          | —                      |
| `MELODY_CRON_K8S_IMAGE`          | `melody.cron.k8s.image`          | `""`                                               | —                      |
| `MELODY_CRON_K8S_NAMESPACE`      | `melody.cron.k8s.namespace`      | `""`                                               | an RFC 1123 label      |
| `MELODY_CRON_K8S_RESTART_POLICY` | `melody.cron.k8s.restart_policy` | `""` (`OnFailure`)                                 | `OnFailure` or `Never` |

An empty `.env` line takes the default on the three keys with a rule and is the empty value on the seven free-text ones: `MELODY_CRON_DESTINATION_FILE=` and `MELODY_CRON_LOGS_DIRECTORY=` leave the generator with no destination and no logs directory, so a run without `--out` is refused and a run without `--logs-directory` refuses an entry that redirects its output.

An application that builds `NewGenerateCommand` in a module of its own, without `cron.Module`, declares the keys from that module's `RegisterEnvironmentKeys` with `cron.EnvironmentKeys(cron.KeyDefaults{})`. A parameter of these names registered by hand is refused at boot.

When the `Configuration` depends on the kernel (resolved parameters, the manager registry, schedules referencing container services) supply a `ConfigurationFactory` instead — it is evaluated at command-registration time, when the kernel exists, and takes precedence over the eager `Configuration`:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    ConfigurationFactory: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return melodycron.NewConfiguration().Schedule(/* … reads kernelInstance.Config() … */)
    },
}))
```

`ModuleConfig.Runner` adds the in-process `melody:cron:run` scheduler beside the generator; its `Commands` are the registered commands the configuration schedules by name. An entry that names no `Timeout` has its context cancelled after the runner's, one hour unless `RunnerConfig.Timeout` names another; an entry that should run with no deadline writes `bound.UnlimitedDuration()`:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration: melodycron.NewConfiguration().
        Schedule("app:report:nightly", melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "2"},
            Timeout:  bound.Of(30 * time.Minute),
        }),
    Runner: melodycron.RunnerConfig{
        Commands: []melodyclicontract.Command{reportCommand},
    },
}))
```

`RunnerConfig` also carries `Dialect`, `OutputCaptureLimit` (64 KiB), `ClockJumpResetThreshold` (3h, at least 2m, whole minutes) and `GracefulTimeout` (5m); each is its default when left out, and a value below its minimum or an unlimited one where the field refuses it panics at boot naming the field.

`NewModule` is available for the v1, v2, v3 and v4 bindings.

## Module dependencies

This module requires:

- `github.com/precision-soft/melody/v4` ≥ v4.0.0 (the minimum this module's [`go.mod`](./go.mod) requires)

Everything else is stdlib — the flag-parsing engine reaches this binding through the framework's cli aliases, as an indirect requirement; no source of this module names it. The package surface is shared across all three bindings — the ownership and user-column surface included (`OwnedTemplate`, `UserColumnTemplate`, `RendersUserColumn`, `CrontabOwnershipMarker`, `ErrBusyboxDivergentDaySchedule`) — apart from three things: this binding additionally ships the built-in `k8s` template (`K8sTemplate`, `TemplateNameK8s`, its `ParameterImage`/`ParameterNamespace`/`ParameterRestartPolicy` constants and its `ErrK8sImageMissing`/`ErrK8sInvalidRestartPolicy`/`ErrK8sInvalidName`/`ErrK8sDuplicateName` errors) and the `Commands(configuration)` helper, and the validators and shell quoting a custom crontab-like dialect renders through (`ValidateScheduleFields`, `ValidateUserField`, `JoinShellTokens`, `ShellQuoteIfNeeded`); it alone gives the in-process runner a zone of its own
(`Configuration.InTimezone`, `Configuration.TimezoneName`, the runner's `--timezone` flag and `ErrUnknownTimezone`), while the generated manifests stay the external scheduler's to place in time; the deprecated abbreviated validation aliases the earlier bindings keep (`ForbiddenChar`, `CrontabForbiddenChars`, `ValidateNoForbiddenChars`) are removed from this one. See [Package surface](../README.md#package-surface) in the umbrella README for the lists, then the [umbrella README](../README.md) for the design details.
