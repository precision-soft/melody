# Melody Bun ORM migration commands

This package exposes Bun migrations as Melody CLI commands (`clicontract.Command` in [`cli/contract/command.go`](../../../cli/contract/command.go)).

It intentionally contains only:

* command construction and flag wiring (see [`RegisterCommands`](./register.go))
* Bun migrate execution per command

Your application is responsible for:

* registering a `*bunorm.ManagerRegistry` service and choosing the default manager (see [`integrations/bunorm/manager_registry.go`](../manager_registry.go))
* providing a `*migrate.Migrations` collection (your app-owned migrations package)
* choosing the migrations directory layout

## Install

In your application module:

```bash
go get github.com/precision-soft/melody/integrations/bunorm/migrate@latest
````

## Usage

### 1) Define your migrations collection

Create a migrations package in your app and expose a `*migrate.Migrations` collection:

```go
package migrations

import "github.com/uptrace/bun/migrate"

var Migrations = migrate.NewMigrations(
	migrate.WithMigrationsDirectory("./database/migrations"),
)
```

### 2) Register commands via a CliModule

Register the commands from a module that implements [`application/contract.CliModule`](../../../application/contract/module.go):

```go
package config

import (
	"your/module/database"
	"your/module/database/migrations"

	applicationcontract "github.com/precision-soft/melody/application/contract"
	clicontract "github.com/precision-soft/melody/cli/contract"
	bunormmigrate "github.com/precision-soft/melody/integrations/bunorm/migrate"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
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

## Options

`RegisterCommands` accepts an [`Options`](./option.go) value.

* `CommandPrefix` (default: `db`) controls the command namespace (`db:init`, `db:migrate`, etc.).
* `ManagerFlagName` (default: `manager`) controls the flag used to select a manager (`--manager` by default).
* `ManagerRegistryServiceId` (default: `service.database.manager.registry`) is the container service id for the `*bunorm.ManagerRegistry`.

Empty values are replaced with the defaults in [`RegisterCommands`](./register.go).

## Commands

All commands support standard Melody output flags (for example `--format=json`).

The manager can be selected with `--<managerFlagName>`. If not provided, the registry default manager is used.

With the default prefix (`db`), the commands are:

* `db:init` – initializes the migrations tables.
* `db:migrate` – applies pending migrations.
* `db:rollback` – rolls back the last migration group.
* `db:status` – shows applied and pending migrations.
* `db:unlock` – unlocks the migrations table.
* `db:create-tx-sql <name>` – creates transactional up/down SQL migration files.
