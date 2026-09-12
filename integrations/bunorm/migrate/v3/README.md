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
* `ManagerRegistryServiceId` (default `service.database.manager.registry`) is the container service id of the `*bunorm.ManagerRegistry`. It selects which registry **instance** these commands resolve, not which implementation: they resolve the concrete `*bunorm.ManagerRegistry`, because `bunorm` publishes no registry contract for them to depend on instead. Point it at a second registry to give a context its own set of managers; there is no door here for substituting a registry of your own.
* `ManagerName` (default empty) pins the manager used when the `--manager` flag is absent; empty falls back to the registry default.

`RegisterCommands` refuses a `nil` migration set rather than answering with no commands: the silence left a
caller believing it had registered the family and finding out at invocation time, as "unknown command".
`NewModule` gates its own optional set before calling, so a binary that registers only `Contexts` never
reaches the refusal — and a `ModuleConfig` carrying neither `Migrations` nor `Contexts` is itself refused by
name at registration.

## Commands

All commands accept the standard Melody output flags. Under `--format=json` a command accumulates its output
and renders one machine-readable json envelope on a single line — the failure included — instead of the
plain-text output; `--format=json-pretty` renders the same document indented for reading by hand, and
`--verbose` and `--no-color` shape the plain-text output alone. The per-query lines a migration prints through `RunQueries` follow the same flags: the command hands its posture to the migrations through the context the migrator passes them, so a migration passes that context on — the generated skeleton does. A migration that drops the context falls back to the process-wide default, which the command installs only for the length of its run; a host that runs migrations on its own sets it through `SetDefaultRunnerOption`.

The json document therefore carries every block the command produces at any verbosity: `--verbose` never
shapes it. Its keys are stable — `data.details`, `data.migrations.applied`, `data.migrations.pending`,
`data.migrations.rolledBack`, `data.database`, `data.files`, `data.messages` — and are not the headings the
text blocks print. `data.database.database` is json `null` when the connection reports no current database,
where the text block renders `<null>`. One consequence is worth knowing: a json run performs the
database-identity query a text run only performs under `--verbose`.

The plain-text rendering escapes every control character visibly — the named C0 ones as `\n`, `\r`, `\t`, every
other C0 one, DEL and the C1 block as `\xNN`, and the Unicode line and paragraph separators as `\uNNNN` —
in every string the commands did not write themselves — the error text off the wire, the failed statement,
the query names, the identity block the server answers and the migration names — and it does so before the
table cells are measured, so the alignment counts the escaped spelling. The failed statement alone keeps its
real line breaks. A byte that is not valid UTF-8 is rendered as `\xNN` of that byte, so a raw C1 introducer off the wire is shown rather than passed through or replaced by U+FFFD. The json rendering is the framework printer's, which spells the C1 block the encoder leaves raw as `\u00NN` escapes; a byte that is not valid UTF-8 reaches the document as U+FFFD, the encoder's documented answer.

The verbose `DATABASE` block is answered for MySQL and for PostgreSQL. Over a unix socket — the connection a
local migration is most likely to take — PostgreSQL reports no server address, so the host reads
`<local socket>`.

All six commands prefer the dedicated migration connection when the registry's provider implements
`bunorm.MigrationProvider`, and fall back to the ordinary pool otherwise: the request pool carries driver
deadlines sized for requests, and a DDL statement that legitimately runs past them is cut mid-statement. The
manager label says which connection carried the run — `(dedicated migration connection)`.

The manager can be selected with `--<managerFlagName>`. Without the flag the `ManagerName` pin from the
registration options is used, and only an empty pin falls back to the registry default — which is the point
of pinning one manager per command set, so a multi-context binary cannot migrate the wrong database by
omitting the flag.

With the default prefix (`db`):

* `db:init` — initializes the migrations tables.
* `db:migrate` — applies pending migrations.
* `db:rollback` — rolls back the last migration group.
* `db:status` — shows applied and pending migrations.
* `db:unlock` — unlocks the migrations table.
* `db:create <migration-name>` — creates a Go migrations file.

Every command runs on the registry's DEDICATED migration connection where the provider offers one — a pool with the driver's read and write deadlines lifted, so a DDL statement that legitimately runs for minutes is not cut mid-statement — and ends it on the way out through the registry's `CloseMigrationDatabase`. That connection recycles nothing by design, so leaving it memoized meant a migration run at the boot of a process that goes on to serve requests kept a deadline-less connection open for the life of that process. A provider offering no migration capability ran on the ordinary pool, which belongs to the application and is left untouched.

`db:create` finishes bun's write ATOMICALLY. Bun creates the file with a single `os.WriteFile` straight onto the destination, so a crash, a full disk or a killed process partway through leaves a truncated Go file in the migrations directory — one that does not compile, under a timestamped name whose place in the sequence looks entirely legitimate. Taking the write away from bun is not possible, since the directory it writes into is unexported; what the command does instead is write the same content into a temporary neighbour, fsync it, rename it over the destination and fsync the DIRECTORY, so a command that reports success has left a whole and durable file behind. A crash before that point leaves bun's partial file — and no success report, which is the answer a failed create has always given. The rewrite keeps the mode the destination carries — bun's 0644 as narrowed by the process umask — rather than stamping 0644 over it. When the rename landed and only the directory fsync failed, the command succeeds with a warning naming the file and that its entry may not survive a crash: a failure verdict would send the operator to run it again and create a second migration beside a whole first one. The name itself is held to `^[0-9a-z_-]+$` before it reaches bun, so a path separator or a parent reference never touches the filesystem.
