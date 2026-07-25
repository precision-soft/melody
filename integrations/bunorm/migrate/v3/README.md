# Melody Bun ORM migration commands (v3)

This module exposes Bun migrations as Melody v3 CLI commands ([`clicontract.Command`](../../../../v3/cli/contract/command.go)).

Import path: `github.com/precision-soft/melody/integrations/bunorm/migrate/v3`

It intentionally contains only:

* command construction and flag wiring (see [`RegisterCommands`](./register.go))
* Bun migrate execution per command

Your application is responsible for:

* registering a `*bunorm.ManagerRegistry` service and choosing the default manager (see the [bunorm README](../../v3/README.md))
* providing a `*migrate.Migrations` collection (your app-owned migrations package)
* choosing the migrations directory layout

## Install

```bash
go get github.com/precision-soft/melody/integrations/bunorm/migrate/v3@latest
```

## Usage

### 1) Define your migrations collection

```go
package migrations

import "github.com/uptrace/bun/migrate"

var Migrations = migrate.NewMigrations()
```

### 2) Register commands from a CliModule

Register the commands from a module that implements [`application/contract.CliModule`](../../../../v3/application/contract/cli_module.go):

```go
package main

import (
    "your/module/database"
    "your/module/database/migrations"

    bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v3"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func (instance *YourModule) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
    return bunormmigrate.RegisterCommands(
        migrations.Migrations,
        bunormmigrate.Options{
            CommandPrefix:            "db",
            ManagerFlagName:          "manager",
            ManagerRegistryServiceId: database.ServiceManagerRegistryId,
        },
    )
}
```

Or bundle it as a self-registering application module, so a single `RegisterModule` call contributes the commands instead of hand-writing `RegisterCliCommands`:

```go
app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
    Migrations: migrations.Migrations,
    Options:    bunormmigrate.Options{CommandPrefix: "db"},
}))
```

`NewModule` is available for the v1, v2, and v3 bindings.

## Multiple database contexts

A binary with several databases declares one context per database and registers the module once; each context gets its own command family pinned to its manager in the shared registry:

```go
app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
    Contexts: []bunormmigrate.ContextConfig{
        {Name: "platform", Migrations: platformmigrations.Migrations},
        {Name: "payment", Migrations: paymentmigrations.Migrations},
    },
}))
```

This generates `db:platform:init|migrate|rollback|status|unlock|create` and the same family under `db:payment:*`. By convention a context's pinned manager name equals the context name (override with `Options.ManagerName`) and its prefix derives from the base prefix (`db:<name>`, override with `Options.CommandPrefix`); every context resolves against the one shared `*bunorm.ManagerRegistry`, so no `container.WithoutTypeRegistration()` juggling is needed. The pin only replaces the registry-default fallback — an explicit `--manager` flag still wins. `Contexts` composes with the legacy single-set `Migrations` field, which keeps its unprefixed commands. For hand-wired setups, `RegisterContextCommands(contexts, baseOptions)` returns the same commands without the module wrapper.

If you deliberately keep separate registries per context, set each context's `ManagerRegistryServiceId`; note that registering a second `*bunorm.ManagerRegistry` service needs `container.WithTypeRegistration(false)` (append; the first provider keeps winning `GetByType`) or `container.WithoutTypeRegistration()` because the strict default refuses a duplicate service type.

## Options

`RegisterCommands` accepts an [`Options`](./option.go) value; empty fields fall back to [`DefaultOptions`](./option.go).

* `CommandPrefix` (default `db`) controls the command namespace (`db:init`, `db:migrate`, …).
* `ManagerFlagName` (default `manager`) controls the manager-selection flag (`--manager`).
* `ManagerRegistryServiceId` (default `service.database.manager.registry`) is the container service id of the `*bunorm.ManagerRegistry`.
* `ManagerName` (default empty) pins the manager used when the `--manager` flag is absent; empty falls back to the registry default.

`RegisterCommands` returns an empty slice when `migrations` is `nil`.

## Commands

All commands accept the standard Melody output flags, but only `--verbose` and `--no-color` affect the output; `--format=json` is not implemented (the commands always print their plain-text output). The manager can be selected with `--<managerFlagName>`; if omitted, the registry default manager is used.

With the default prefix (`db`):

* `db:init` — initializes the migrations tables.
* `db:migrate` — applies pending migrations.
* `db:rollback` — rolls back the last migration group.
* `db:status` — shows applied and pending migrations.
* `db:unlock` — unlocks the migrations table.
* `db:create <migration-name>` — creates a Go migrations file.
