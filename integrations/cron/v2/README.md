# Cron generator — Melody v2 binding

Melody v2 binding for the [`precision-soft/melody/integrations/cron`](..) crontab generator. See the [umbrella README](../README.md) for the full design, configuration parameters, cascade rules, per-command overrides, custom heartbeat command, [template customization](../README.md#customizing-the-template) (plug your own `Template` for Kubernetes / Supervisor / ...) and footguns.

## Install

```bash
go get github.com/precision-soft/melody/integrations/cron/v2@latest
```

## Import paths

```go
import (
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)
```

## Quick wiring

See the [umbrella README](../README.md#configuration-parameters) for the full wiring walk-through (`cron.Configuration` registry, `RegisterDefaultParameters`, `NewGenerateCommand(configuration)`, parameter cascade, per-command overrides). The same examples apply verbatim with the v2 import paths above.

## Module dependencies

This module requires:

* `github.com/precision-soft/melody/v2` ≥ v2.0.0
* `github.com/urfave/cli/v3` ≥ v3.6.1

Everything else is stdlib. The package surface is identical across all three bindings — see [Package surface](../README.md#package-surface) in the umbrella README for the full list, then the [umbrella README](../README.md) for the design details.
