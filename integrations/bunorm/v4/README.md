# Bun ORM integration (v4)

This directory contains the **optional Bun ORM integration** for Melody v4.

The integration is split into independent Go modules so consumers depend only on what they need:

- Core (dialect-agnostic): [`./`](./)
- MySQL provider: [`../mysql/v4/`](../mysql/v4/)
- PostgreSQL provider: [`../pgsql/v4/`](../pgsql/v4/)
- Migration CLI commands: [`../migrate/v4/`](../migrate/v4/)

Import path: `github.com/precision-soft/melody/integrations/bunorm/v4`

## What you get

- A dialect-agnostic **manager registry** ([`bunorm.ManagerRegistry`](./manager_registry.go)) that:
    - Caches one [`bunorm.Manager`](./manager.go) **1:1** per provider definition ([`bunorm.ProviderDefinition`](./provider_definition.go)).
    - Supports **exactly one default** provider (error if multiple defaults).
    - Falls back to the **first** provider as default if none is marked.
- A [`bunorm.Manager`](./manager.go) that owns a single Bun database handle and exposes `DefinitionName()`, `Database() *bun.DB`, and `CloseWithContext(context.Context) error`, which waits for the database's close up to the deadline and answers the deadline's error past it while the close goes on in the background.
- A [`bunorm.ReadWriteSplitter`](./split.go) that routes writes to a primary manager and reads to one or more replica managers held in the same registry.

## Connection parameters

A provider opens a database from an explicit [`bunorm.ConnectionParameters`](./connection_parameters.go) value (`Host`, `Port`, `Database`, `User`, `Password`) — the caller reads these from config/environment and passes them in. `SafeContext()` returns the parameters with the password omitted, suitable for logging.

The [`bunorm.Provider`](./provider.go) contract has one open door per purpose, each taking the runtime it runs under first:

```go
Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
```

`Open` answers a fresh pool for request traffic, whose ownership passes to the registry. `OpenForMigration` answers the connection the migration commands run on, with whatever the provider tunes for a DDL statement that runs for minutes (the `mysql` and `pgsql` providers lift their read and write deadlines); a provider with nothing to tune answers its `Open`:

```go
func (instance *Provider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}
```

A provider refuses a nil runtime, or one carrying no context, and refuses a context already done before its attempt; its retry wait watches the runtime's context beside the clock.

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

    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v4"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v4"
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/logging"
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
                logging.LoggerMustFromResolver(resolver.Runtime().Context(), resolver),
                []melodybunorm.ProviderDefinition{
                    melodybunorm.ProviderDefinition{
                        Name:     ManagerPrimaryName,
                        Provider: melodymysql.NewProvider(melodymysql.ProviderConfig{}),
                        Parameters: melodybunorm.ConnectionParameters{
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
                        Provider: melodymysql.NewProvider(melodymysql.ProviderConfig{}),
                        Parameters: melodybunorm.ConnectionParameters{
                            Host:     os.Getenv("ADMIN_DB_HOST"),
                            Port:     os.Getenv("ADMIN_DB_PORT"),
                            Database: os.Getenv("ADMIN_DB_DATABASE"),
                            User:     os.Getenv("ADMIN_DB_USER"),
                            Password: os.Getenv("ADMIN_DB_PASSWORD"),
                        },
                    },
                },
            )
        },
        containercontract.RegisterOptions{},
    )

    registrar.RegisterService(
        ServiceManagerDefaultId,
        func(resolver containercontract.Resolver) (*melodybunorm.Manager, error) {
            registry := container.MustFromResolver[*melodybunorm.ManagerRegistry](resolver.Runtime().Context(), resolver, ServiceManagerRegistryId)

            return melodybunorm.For(resolver.Runtime(), registry).DefaultManager()
        },
        containercontract.RegisterOptions{},
    )
}
```

[`NewManagerRegistry`](./manager_registry.go) takes a `logging/contract.Logger` (it is handed to every `Provider.Open` and `Provider.OpenForMigration`), then the provider definitions as a slice; it captures no context. It fails with `ErrLoggerIsRequired` on a nil logger and `ErrNoProviderDefinitions` when no definition is given, and refuses a definition by its position and name, in this order: an empty name, a nil provider, an empty `Parameters.Host` (`ErrConnectionHostIsRequired`) or `Parameters.Database` (`ErrConnectionDatabaseIsRequired`), the field named in the refusal's context, a duplicate name, and more than one default. `User` and `Password` stay free, since a socket or a trust-authenticated local database has neither, and an empty `Port` dials the driver's default. Because this major hands providers the connection values rather than the configuration keys they came from, this registry declares no credential of its own and carries no marking door: arming the framework's redaction is the application's call, through the parameter registrar's own `RegisterSecretParameter` for a parameter the application declares and
a `.env` key declared `Secret` in `ApplicationConfig.EnvironmentKeys`, which marks the parameter it feeds; `MarkParameterSecret` takes a parameter name, never a key. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it and `debug:parameters` masks the password in a process that never opens a connection. Opening a connection also routes bun's own diagnostic channel into the application's journal through `RouteDiagnostics`.

Bun's logger is one variable for the whole process, so it is SET once — but what is set is a forwarder onto a destination `RouteDiagnostics` replaces whenever it is called with a different logger, and the destination is what decides where a record goes; a routing on the logger already installed changes nothing, so the providers routing on every open allocate nothing. A process that builds, closes and rebuilds its application therefore has each lifecycle take the channel back, where the first one used to keep it for the life of the process and every later lifecycle's diagnostics were dropped into a logger that was closed. `ResetDiagnostics()` hands the channel back to standard error, which is where bun writes when nobody routes it at all; it is the door for a host process that keeps its own reporting for bun. The registry's own `CloseWithContext` hands the channel back only when it is its own — routed to the registry's logger — so a second registry in the same process keeps its channel through the
first one's teardown. Two registries built on one logger are two owners, not one: each hands its providers the logger under an identity of its own, so whichever opened last holds the channel, and a close hands it back only while that registry's own routing is the live one — the survivor's routing is left in place, and its next open takes the channel again.

Every door that opens takes the runtime it runs under first — `Manager(runtimeInstance, name)`, `MustManager`, `Database`, `MustDatabase`, `DefaultManager(runtimeInstance)`, `MustDefaultManager`, `DefaultDatabase`, `MustDefaultDatabase` and `MigrationDatabase(runtimeInstance, name)` — and the provider is handed that runtime over a context the caller's own context and the registry's `CloseWithContext` both end, so a request that goes away, a cli run that is cancelled or a teardown stops a retry loop instead of sleeping through its whole budget. A nil runtime, a typed nil and one carrying no context are refused with "the runtime is nil or carries no context", one carrying no scope with "the runtime carries no scope", before the registry is locked; a caller whose context is already done is refused before an open starts, nothing dialled, held or memoized, and a manager already opened is answered whatever the context. Concurrent callers of one name coalesce onto one open, the first caller's: a waiter whose own context ends leaves with that context's cause, the open going on for its opener, and when the open ended by its opener's context, a waiter whose context is still live opens again, so no caller ever receives another caller's cancellation. A failed open is never memoized. `HasProviderDefinition`, `CloseMigrationDatabase` and `CloseWithContext` take no runtime.

The logger is written once, at construction, so the registry is built where the application's logger exists — inside its provider, as the sample does, resolving the logging service. A nil logger, and a typed nil holding no value, are refused with `ErrLoggerIsRequired`.

A provider receives the registry's own logger, which carries the application's under an identity the registry owns: read it through `loggingcontract.Logger` and do not assert its concrete type. It forwards the logger's level threshold, and it is how the registry recognises a routing made through it whatever the logger's own comparability — a logger held by value, carrying a slice, a map or a func, is recognised at the registry's `CloseWithContext` like any other. `CloseWithContext` retires the logger handed out, and a routing through a retired logger routes nothing, so an open still in flight when the registry closed cannot take bun's channel back onto it.

**Register the registry with a provider that RESOLVES the logging service rather than one that captures a pre-built registry.** The container records a teardown dependency at the moment one service resolves another and closes dependents before their dependencies, so a resolving provider gets the registry closed ahead of the journal for free — which is what lets its `CloseWithContext` tear down the pools and hand bun's channel back while the journal is still open. A capturing provider resolves nothing, declares nothing, and leaves the two closes ordered by coincidence; where a provider genuinely has nothing to resolve, the registration's `RegisterOptions.TeardownDependencyNames` writes the same edge declaratively.

`Close` ends every pool the registry memoized, then WAITS for the opens that were still in flight when it published its refusal — and it ENDS them: the refusal fires the registry's close signal, which ends the context of every open in flight and which every provider of this repository honours in its retry sleeps and its dials, so an open in flight returns instead of dialling on, an open that returns ends its own freshly opened database against the closed flag, and nothing leaks. `Close` used to leave them running under the registry's context, each further attempt routing bun's diagnostics back onto the logger the teardown had just handed back, and to return while a dial was still in the air — a process exiting on that answer left the connection outstanding, its server-side session to be reaped by a timeout rather than ended. The wait is unbounded on `Close` and ends with the caller's deadline on `CloseWithContext`, which is the door the framework container's ordered teardown prefers: a dial a driver does not cut on cancellation
ends when the driver gives up, which can outlive a teardown, so the answer then names the opens it stopped waiting for rather than having ended them.

`MigrationDatabase(runtimeInstance, name)` answers the connection the provider's `OpenForMigration` opened for the definition, opened once per name and shared. A provider with nothing to tune answers its `Open` there, a second pool kept apart from the request pool; one that answers the very `*bun.DB` the registry already holds as that name's pool is recognised by identity and shared with it, neither kept as a migration connection nor closed by a release. `CloseMigrationDatabase(name)` releases one hold on the dedicated migration connection for one definition — every `MigrationDatabase` takes a hold, so two migration commands in one process share the connection — and with the last hold ends it and forgets it, so the next `MigrationDatabase` for that name opens a fresh one. That connection is not a request pool and must not live like one: it deliberately lifts the driver's read and write deadlines and recycles nothing, which is right for a DDL statement that runs for minutes and wrong for anything that then sits idle. The migration commands call it on their way out; the registry's `CloseWithContext` stays the net underneath for whatever did not. An empty name selects the default definition, a name with no migration connection is not an error, and a closed registry refuses it.

[`HasProviderDefinition(name)`](./manager_registry.go) answers whether a definition is registered under the name, without opening anything — the question of a command that only labels a manager. The `migrate` module's `db:create` asks it, since it writes its file from the migrations collection alone and no longer opens the database that used to validate `--manager`.

### Consuming the default database

```go
package main

import melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v4"

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
adminManager := melodybunorm.FromRuntime(runtimeInstance, ServiceManagerRegistryId).MustManager(ManagerAdminName)
adminDatabase := adminManager.Database()
```

### The bound registry

A handler, a command or a subscriber calls the registry through [`bunorm.Bound`](./bound.go), the registry bound to the runtime it runs under: its nine doors, `Manager`, `MustManager`, `Database`, `MustDatabase`, `DefaultManager`, `MustDefaultManager`, `DefaultDatabase`, `MustDefaultDatabase` and `MigrationDatabase`, take no runtime, and `Runtime()` and `Registry()` answer the two halves. `FromRuntime(runtimeInstance, serviceName)` resolves the registry the application registered under its own name through the runtime's scope and binds it, the module registering none; `For(runtimeInstance, registry)` binds a registry already in hand and allocates nothing. The view is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field; a call that needs another deadline binds again over `runtime.WithContext`.

```go
boundRegistry := melodybunorm.FromRuntime(runtimeInstance, ServiceManagerRegistryId)

reportDatabase, openErr := boundRegistry.Database(ManagerAdminName)
```

A zero view, or one over no registry, refuses its five error doors with `manager registry is nil`, and its four `Must…` doors panic with that refusal. `MigrationDatabase` takes its hold through the view; the hold is released through `boundRegistry.Registry().CloseMigrationDatabase(name)`, which takes no runtime, as `HasProviderDefinition` and `CloseWithContext` do. The splitter has no view: `splitter.Reader(runtimeInstance)` is one call per query function. The wiring generator refuses a constructor argument of type `bunorm.Bound`, by value or by pointer: bind it inside the method, from the runtime.

### Read/write splitting

```go
splitter := melodybunorm.NewReadWriteSplitter(registry, ManagerPrimaryName, melodybunorm.SplitterConfig{
    ReplicaNames: []string{"replica-1", "replica-2"},
})

writer, _ := splitter.Writer(runtimeInstance)  // always the primary
reader, _ := splitter.Reader(runtimeInstance)  // a replica (or the primary if none configured)
```

`Reader` round-robins the replicas and falls back to the primary only when a replica is **unreachable** — an open failure its provider classified as transient and could not get past, answered by `ErrDatabaseUnreachable`: the outage where reading from the primary is the availability trade the splitter exists to make. Every other failure is refused instead of absorbed — a replica name the registry does not know, an empty name, a closed registry, a provider that answered neither a database nor an error, and every refusal the provider or the server gave by name (a parameter left empty, a password the server refused, a database that does not exist): each is a wiring error, permanent by nature, and folding it into the fallback routed every read to the primary forever with no signal. The `pgsql` and `mysql` providers file an outage under the class through `DatabaseUnreachable`; a provider of your own that files nothing has every failure read as a refusal, the direction that surfaces a
misconfiguration. When the primary then fails too, the error carries the primary failure as its cause and names the replica failure beside it. Both doors open under the caller's runtime, and a replica open the caller's own context ended is answered as that refusal: the caller left, so the replica is not remembered unreachable and the primary is not opened in its place. An empty replica name is refused at construction, naming its index.

An unreachable replica is remembered for `SplitterConfig.ReplicaRetryInterval`, five seconds when left out, at least 1ms, unlimited refused, since a replica never dialled again is a silent primary-only split: inside it the replica's share of the reads goes to the primary without dialling it again, so a dead replica no longer costs every read a connect timeout, and the first read after it dials the replica again. The move is journaled once each way through the registry's logger, a warning naming the replica, the primary and the interval when reads move to the primary, and an info when the replica takes them back:

```go
splitter := melodybunorm.NewReadWriteSplitter(registry, ManagerPrimaryName, melodybunorm.SplitterConfig{
    ReplicaNames:         []string{"replica-1", "replica-2"},
    ReplicaRetryInterval: bound.Of(30 * time.Second),
})
```

## Dialect providers

- MySQL provider: [`../mysql/v4/`](../mysql/v4/)
- PostgreSQL provider: [`../pgsql/v4/`](../pgsql/v4/)

Each dialect module implements [`bunorm.Provider`](./provider.go): it builds the driver connector, constructs a Bun database handle with the correct dialect, and performs an initial `PingContext`, failing fast on an error unless a `RetryConfig` retries a transient one with backoff. Both expose typed `PoolConfig`/`TimeoutConfig` and an optional post-build hook for driver options not surfaced by the typed configs.
