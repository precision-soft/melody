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

Register the commands from a module that implements [`application/contract.CliModule`](../../../../v3/application/contract/module.go):

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

## Options

`RegisterCommands` accepts an [`Options`](./option.go) value; empty fields fall back to [`DefaultOptions`](./option.go).

* `CommandPrefix` (default `db`) controls the command namespace (`db:init`, `db:migrate`, …).
* `ManagerFlagName` (default `manager`) controls the manager-selection flag (`--manager`).
* `ManagerRegistryServiceId` (default `service.database.manager.registry`) is the container service id of the `*bunorm.ManagerRegistry`.

`RegisterCommands` returns an empty slice when `migrations` is `nil`.

## Commands

All commands support standard Melody output flags (for example `--format=json`). The manager can be selected with `--<managerFlagName>`; if omitted, the registry default manager is used.

With the default prefix (`db`):

* `db:init` — initializes the migrations tables.
* `db:migrate` — applies pending migrations.
* `db:rollback` — rolls back the last migration group.
* `db:status` — shows applied and pending migrations.
* `db:unlock` — unlocks the migrations table.
* `db:create <migration-name>` — creates a Go migrations file.
