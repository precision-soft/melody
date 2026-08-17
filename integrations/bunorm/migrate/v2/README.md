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

Register the commands from a module that implements [`application/contract.CliModule`](../../../../v2/application/contract/cli_module.go):

```go
package main

import (
	"your/module/database"
	"your/module/database/migrations"

	applicationcontract "github.com/precision-soft/melody/v2/application/contract"
	clicontract "github.com/precision-soft/melody/v2/cli/contract"
	bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate/v2"
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

Or bundle it as a self-registering application module, so a single `RegisterModule` call contributes the commands instead of hand-writing `RegisterCliCommands`:

```go
app.RegisterModule(bunormmigrate.NewModule(bunormmigrate.ModuleConfig{
    Migrations: migrations.Migrations,
    Options:    bunormmigrate.Options{CommandPrefix: "db"},
}))
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

This generates `db:platform:init|migrate|rollback|status|unlock|create` and the same family under `db:payment:*`. By convention a context's pinned manager name equals the context name (override with `Options.ManagerName`) and its prefix derives from the base prefix (`db:<name>`, override with `Options.CommandPrefix`); every context resolves against the one shared `*bunorm.ManagerRegistry`. The pin only replaces the registry-default fallback — an explicit `--manager` flag still wins. `Contexts` composes with the legacy single-set `Migrations` field, which keeps its unprefixed commands. For hand-wired setups, `RegisterContextCommands(contexts, baseOptions)` returns the same commands without the module wrapper.

If you deliberately keep separate registries per context, set each context's `ManagerRegistryServiceId`.

## Options

`RegisterCommands` accepts an [`Options`](./option.go) value.

* `CommandPrefix` (default: `db`) controls the command namespace (`db:init`, `db:migrate`, etc.).
* `ManagerFlagName` (default: `manager`) controls the flag used to select a manager (`--manager` by default).
* `ManagerRegistryServiceId` (default: `service.database.manager.registry`) is the container service id for the `*bunorm.ManagerRegistry`. It selects which registry **instance** these commands resolve, not which implementation: the commands resolve the concrete `*bunorm.ManagerRegistry`, because `bunorm` publishes no registry contract for them to depend on instead. Point it at a second registry to give a context its own set of managers; there is no door here for substituting a registry of your own.
* `ManagerName` (default: empty) pins the manager used when the `--manager` flag is absent; empty falls back to the registry default.

Empty values are replaced with the defaults in [`RegisterCommands`](./register.go), and [`DefaultOptions`](./option.go) answers with that same set as a value — the way to read what a field falls back to, or to start from it and change one field.

## Commands

All commands accept the standard Melody output flags. Under `--format=json` a command accumulates its output and renders one machine-readable json envelope on a single line — a failure included — instead of the plain-text output; `--format=json-pretty` renders the same document indented for reading by hand, and `--verbose` and `--no-color` affect the plain-text output alone.

The json document therefore carries every block the command produces at any verbosity: `--verbose` never shapes it. Its keys are stable — `data.details`, `data.migrations.applied`, `data.migrations.pending`, `data.migrations.rolledBack`, `data.database`, `data.files`, `data.messages` — and are not the headings the text blocks print. `data.database.database` is json `null` when the connection reports no current database, where the text block renders `<null>`. One consequence is worth knowing: a json run performs the database-identity query a text run only performs under `--verbose`.

The manager can be selected with `--<managerFlagName>`. Without the flag the `ManagerName` pin from the registration options is used, and only an empty pin falls back to the registry default — which is the point of pinning one manager per command set, so a multi-context binary cannot migrate the wrong database by omitting the flag.

With the default prefix (`db`), the commands are:

* `db:init` – initializes the migrations tables.
* `db:migrate` – applies pending migrations.
* `db:rollback` – rolls back the last migration group.
* `db:status` – shows applied and pending migrations.
* `db:unlock` – unlocks the migrations table.
* `db:create <migration-name>` – creates a go migrations file.
