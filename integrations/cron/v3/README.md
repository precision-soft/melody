# Cron generator — Melody v3 binding

Melody v3 binding for the [`precision-soft/melody/integrations/cron`](..) crontab generator. See the [umbrella README](../README.md) for the full design, configuration parameters, cascade rules, per-command overrides, custom heartbeat command, [template customization](../README.md#customizing-the-template) (plug your own `Template` for Kubernetes / Supervisor / ...) and footguns.

## Install

```bash
go get github.com/precision-soft/melody/integrations/cron/v3@latest
```

## Import paths

```go
import (
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)
```

## Quick wiring

See the [umbrella README](../README.md#configuration-parameters) for the full wiring walk-through (`cron.Configuration` registry, `RegisterDefaultParameters`, `NewGenerateCommand(configuration)`, parameter cascade, per-command overrides). The same examples apply verbatim with the v3 import paths above.

Or bundle it as a self-registering application module — one `RegisterModule` call contributes the crontab-generation command and, opt-in, the default parameters:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    Configuration:         configuration,
    WithDefaultParameters: true,
}))
```

When the `Configuration` depends on the kernel (resolved parameters, the manager registry, schedules referencing container services) supply a `ConfigurationFactory` instead — it is evaluated at command-registration time, when the kernel exists, and takes precedence over the eager `Configuration`:

```go
app.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{
    ConfigurationFactory: func(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
        return melodycron.NewConfiguration().Schedule(/* … reads kernelInstance.Config() … */)
    },
}))
```

`NewModule` is available for the v1, v2, and v3 bindings.

## Module dependencies

This module requires:

* `github.com/precision-soft/melody/v3` ≥ v3.6.0 (the minimum this module's [`go.mod`](./go.mod) requires)
* `github.com/urfave/cli/v3` ≥ v3.6.1

Everything else is stdlib. The package surface is shared across all three bindings — the ownership and user-column surface included (`OwnedTemplate`, `UserColumnTemplate`, `RendersUserColumn`, `CrontabOwnershipMarker`, `ErrBusyboxDivergentDaySchedule`) — apart from three things: this binding additionally ships the built-in `k8s` template (`K8sTemplate`, `TemplateNameK8s`, its `ParameterImage`/`ParameterNamespace`/`ParameterRestartPolicy` constants and its `ErrK8sImageMissing`/`ErrK8sInvalidRestartPolicy`/`ErrK8sInvalidName`/`ErrK8sDuplicateName` errors) and the `Commands(configuration)` helper; it alone gives the in-process runner a zone of its own (`Configuration.InTimezone`, `Configuration.TimezoneName`, the runner's `--timezone` flag and `ErrUnknownTimezone`), while the generated manifests stay the external scheduler's to place in time; and it alone has removed the deprecated abbreviated validation aliases the frozen majors keep. See [Package surface](../README.md#package-surface) in the umbrella README for the lists, then the [umbrella README](../README.md) for the design details.
