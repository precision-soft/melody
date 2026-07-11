# Melody Bun ORM migration commands

This package exposes Bun migrations as Melody CLI commands (`clicontract.Command` in [`cli/contract/command.go`](../../../../v2/cli/contract/command.go)).

It intentionally contains only:

* command construction and flag wiring (see [`RegisterCommands`](./register.go))
* Bun migrate execution per command

Your application is responsible for:

* registering a `*bunorm.ManagerRegistry` service and choosing the default manager (see [`integrations/bunorm/v2/manager_registry.go`](../../v2/manager_registry.go))
* providing a `*migrate.Migrations` collection (your app-owned migrations package)
* choosing the migrations directory layout

## Install

In your application module:

```bash
go get github.com/precision-soft/melody/integrations/bunorm/migrate/v2@latest
```

## Usage

### 1) Define your migrations collection

Create a migrations package in your app and expose a `*migrate.Migrations` collection:

```go
package main

import "github.com/uptrace/bun/migrate"

var Migrations = migrate.NewMigrations()
```

### 2) Register commands via a CliModule

Register the commands from a module that implements [`application/contract.CliModule`](../../../../v2/application/contract/module.go):

```go
package main

import (
	"your/module/database"
	"your/module/database/migrations"

	bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v2"
	applicationcontract "github.com/precision-soft/melody/v2/application/contract"
	clicontract "github.com/precision-soft/melody/v2/cli/contract"
	kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func (instance *YourModule) RegisterCliCommands(kernelInstance kernelcontract.Kernel) []clicontract.Command {
	commands := make([]clicontract.Command, 0)

	commands = append(
		commands,
		bunormmigrate.RegisterCommands(
			migrations.Migrations,
			bunormmigrate.Options{
				CommandPrefix:            "db",
				ManagerFlagName:          "manager",
				ManagerRegistryServiceId: database.ServiceManagerRegistryId,
			},
		)...,
	)

	return commands
}

var _ applicationcontract.CliModule = (*YourModule)(nil)
```

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

`RegisterCommands` accepts an [`Options`](./option.go) value.

* `CommandPrefix` (default: `db`) controls the command namespace (`db:init`, `db:migrate`, etc.).
* `ManagerFlagName` (default: `manager`) controls the flag used to select a manager (`--manager` by default).
* `ManagerRegistryServiceId` (default: `service.database.manager.registry`) is the container service id for the `*bunorm.ManagerRegistry`.
* `ManagerName` (default: empty) pins the manager used when the `--manager` flag is absent; empty falls back to the registry default.

Empty values are replaced with the defaults in [`RegisterCommands`](./register.go).

## Commands

All commands accept the standard Melody output flags, but only `--verbose` and `--no-color` affect the output; `--format=json` is not implemented (the commands always print their plain-text output).

The manager can be selected with `--<managerFlagName>`. If not provided, the registry default manager is used.

With the default prefix (`db`), the commands are:

* `db:init` – initializes the migrations tables.
* `db:migrate` – applies pending migrations.
* `db:rollback` – rolls back the last migration group.
* `db:status` – shows applied and pending migrations.
* `db:unlock` – unlocks the migrations table.
* `db:create <migration-name>` – creates a go migrations file.
