# Bun ORM integration (v3)

This directory contains the **optional Bun ORM integration** for Melody v3.

The integration is split into independent Go modules so consumers depend only on what they need:

* Core (dialect-agnostic): [`./`](./)
* MySQL provider: [`../mysql/v3/`](../mysql/v3/)
* PostgreSQL provider: [`../pgsql/v3/`](../pgsql/v3/)
* Migration CLI commands: [`../migrate/v3/`](../migrate/v3/)

Import path: `github.com/precision-soft/melody/integrations/bunorm/v3`

## What you get

* A dialect-agnostic **manager registry** ([`bunorm.ManagerRegistry`](./manager_registry.go)) that:
    * Caches one [`bunorm.Manager`](./manager.go) **1:1** per provider definition ([`bunorm.ProviderDefinition`](./provider_definition.go)).
    * Supports **exactly one default** provider (error if multiple defaults).
    * Falls back to the **first** provider as default if none is marked.
* A [`bunorm.Manager`](./manager.go) that owns a single Bun database handle and exposes `DefinitionName()`, `Database() *bun.DB`, and `Close() error`.
* A [`bunorm.ReadWriteSplitter`](./split.go) that routes writes to a primary manager and reads to one or more replica managers held in the same registry.

## Connection parameters

A provider opens a database from an explicit [`bunorm.ConnectionParameters`](./connection_parameters.go) value (`Host`, `Port`, `Database`, `User`, `Password`) — the caller reads these from config/environment and passes them in. `SafeContext()` returns the parameters with the password omitted, suitable for logging.

The [`bunorm.Provider`](./provider.go) contract is a single method:

```go
Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
```

## Usage

The pattern is:

1. Register a `*bunorm.ManagerRegistry` service (explicit id), built from one `ProviderDefinition` per database.
2. Register the default `*bunorm.Manager` as a service that can be autowired by type.
3. Consume `*bunorm.Manager` (default) in your services/handlers.
4. Optionally, resolve the registry and request a named manager for a non-default database.

### Service registration example

```go
package main

import (
    "os"

    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
)

const (
    ServiceManagerRegistryId = "service.database.manager.registry"
    ServiceManagerDefaultId  = "service.database.manager.default"

    ManagerPrimaryName = "primary"
    ManagerAdminName   = "admin"
)

func RegisterDatabaseServices(registrar applicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        ServiceManagerRegistryId,
        func(resolver containercontract.Resolver) (*melodybunorm.ManagerRegistry, error) {
            return melodybunorm.NewManagerRegistry(
                logging.LoggerMustFromResolver(resolver),
                melodybunorm.ProviderDefinition{
                    Name:     ManagerPrimaryName,
                    Provider: melodymysql.NewProvider(),
                    Params: melodybunorm.ConnectionParameters{
                        Host:     os.Getenv("DB_HOST"),
                        Port:     os.Getenv("DB_PORT"),
                        Database: os.Getenv("DB_DATABASE"),
                        User:     os.Getenv("DB_USER"),
                        Password: os.Getenv("DB_PASSWORD"),
                    },
                    IsDefault: true,
                },
                melodybunorm.ProviderDefinition{
                    Name:     ManagerAdminName,
                    Provider: melodymysql.NewProvider(),
                    Params: melodybunorm.ConnectionParameters{
                        Host:     os.Getenv("ADMIN_DB_HOST"),
                        Port:     os.Getenv("ADMIN_DB_PORT"),
                        Database: os.Getenv("ADMIN_DB_DATABASE"),
                        User:     os.Getenv("ADMIN_DB_USER"),
                        Password: os.Getenv("ADMIN_DB_PASSWORD"),
                    },
                },
            )
        },
    )

    registrar.RegisterService(
        ServiceManagerDefaultId,
        func(resolver containercontract.Resolver) (*melodybunorm.Manager, error) {
            registry := container.MustFromResolver[*melodybunorm.ManagerRegistry](resolver, ServiceManagerRegistryId)

            return registry.DefaultManager()
        },
    )
}
```

[`NewManagerRegistry`](./manager_registry.go) takes a `logging/contract.Logger` as its first argument (it is handed to every `Provider.Open`), then the provider definitions as variadic values. It fails with `ErrLoggerIsRequired` on a nil logger and `ErrNoProviderDefinitions` when no definition is given. `NewManagerRegistryWithContext` additionally binds the registry to a context its lazy opens run under: a provider that implements `ContextOpener` refuses an open the context already cancelled and has its retry sleeps watch the context alongside the clock, and the same preference reaches the migration open through `MigrationContextOpener`. Because this major hands providers the connection values rather than the configuration keys they came from, this registry declares no credential of its own and carries no marking door: arming the framework's redaction is the application's call, through the parameter registrar's own `RegisterSecretParameter` for a parameter the application declares and `MarkParameterSecret` for one melody registered from the `.env` artifacts. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it and `debug:parameters` masks the password in a process that never opens a connection. Opening a connection also routes bun's own diagnostic channel into the application's journal through `RouteDiagnostics`.

Bun's logger is one variable for the whole process, so it is SET once — but what is set is a forwarder onto a destination `RouteDiagnostics` replaces whenever it is called with a different logger, and the destination is what decides where a record goes; a routing on the logger already installed changes nothing, so the providers routing on every open allocate nothing. A process that builds, closes and rebuilds its application therefore has each lifecycle take the channel back, where the first one used to keep it for the life of the process and every later lifecycle's diagnostics were dropped into a logger that was closed. `ResetDiagnostics()` hands the channel back to standard error, which is where bun writes when nobody routes it at all; it is the door for a host process that keeps its own reporting for bun. The registry's own `Close` hands the channel back only when it is its own — routed to the registry's logger — so a second registry in the same process keeps its channel through the first one's teardown; two registries sharing one logger share one channel, and the next open of the survivor takes it again.

`SetLogger` replaces the logger a registry reports through, and takes bun's diagnostic channel with it, so the two cannot drift apart. It exists for the wiring window an application cannot avoid: a registry assembled while the modules are still being wired has no application logger to be handed — the framework's own does not exist yet — so it is built on the emergency logger, and without this door every later open, every retry warning and every terminal connection failure would keep bypassing the journal. A nil logger, and a typed nil holding no value, are refused with `ErrLoggerIsRequired`.

**Register the registry with a provider that RESOLVES the logging service rather than one that captures a pre-built registry.** The container records a teardown dependency at the moment one service resolves another and closes dependents before their dependencies, so a resolving provider gets the registry closed ahead of the journal for free — which is what lets its `Close` tear down the pools and hand bun's channel back while the journal is still open. A capturing provider resolves nothing, declares nothing, and leaves the two closes ordered by coincidence; where a provider genuinely has nothing to resolve, the container's `WithTeardownDependency` writes the same edge declaratively.

`Close` ends every pool the registry memoized, then WAITS for the opens that were still in flight when it published its refusal. Each of those ends its own freshly opened database against the closed flag, so nothing leaked before either — but `Close` used to return while a dial was still in the air, and a process exiting on that answer left the connection outstanding, its server-side session to be reaped by a timeout rather than ended. The wait is unbounded by design; `NewManagerRegistryWithContext` is the door that shortens it, because cancelling that context reaches the open in flight.

`CloseMigrationDatabase(name)` ends the dedicated migration connection for one definition and forgets it, so the next `MigrationDatabase` for that name opens a fresh one. That connection is not a request pool and must not live like one: it deliberately lifts the driver's read and write deadlines and recycles nothing, which is right for a DDL statement that runs for minutes and wrong for anything that then sits idle. The migration commands call it on their way out; the registry's `Close` stays the net underneath for whatever did not. An empty name selects the default definition, a name with no migration connection is not an error, and a closed registry refuses it.

### Consuming the default database

```go
package main

import melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"

type ApiService struct {
    databaseManager *melodybunorm.Manager
}

func NewApiService(databaseManager *melodybunorm.Manager) *ApiService {
    return &ApiService{databaseManager: databaseManager}
}

func (instance *ApiService) Database() {
    database := instance.databaseManager.Database()
    _ = database
}
```

### Consuming a non-default database

```go
adminManager := registry.MustManager(ManagerAdminName)
adminDatabase := adminManager.Database()
```

### Read/write splitting

```go
splitter := melodybunorm.NewReadWriteSplitter(registry, ManagerPrimaryName, "replica-1", "replica-2")

writer, _ := splitter.Writer()  // always the primary
reader, _ := splitter.Reader()  // a replica (or the primary if none configured)
```

`Reader` round-robins the replicas and falls back to the primary only when a replica fails to **open** — a transient outage, where reading from the primary is the availability trade the splitter exists to make. A replica name the registry does not know, an empty name and a closed registry are refused instead of absorbed: each is a wiring error, permanent by nature, and folding it into the fallback would route every read to the primary forever with no signal. When the primary then fails too, the error carries the primary failure as its cause and names the replica failure beside it. An empty replica name is refused at construction.

## Dialect providers

* MySQL provider: [`../mysql/v3/`](../mysql/v3/)
* PostgreSQL provider: [`../pgsql/v3/`](../pgsql/v3/)

Each dialect module implements [`bunorm.Provider`](./provider.go): it builds the driver connector, constructs a Bun database handle with the correct dialect, and performs an initial `PingContext`, failing fast on errors. Both expose typed `PoolConfig`/`TimeoutConfig` and an optional post-build hook for driver options not surfaced by the typed configs.
