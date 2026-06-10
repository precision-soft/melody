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

A provider opens a database from an explicit [`bunorm.ConnectionParams`](./connection_params.go) value (`Host`, `Port`, `Database`, `User`, `Password`) — the caller reads these from config/environment and passes them in. `SafeContext()` returns the parameters with the password omitted, suitable for logging.

The [`bunorm.Provider`](./provider.go) contract is a single method:

```go
Open(params ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)
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
                    Params: melodybunorm.ConnectionParams{
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
                    Params: melodybunorm.ConnectionParams{
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

## Dialect providers

* MySQL provider: [`../mysql/v3/`](../mysql/v3/)
* PostgreSQL provider: [`../pgsql/v3/`](../pgsql/v3/)

Each dialect module implements [`bunorm.Provider`](./provider.go): it builds the driver connector, constructs a Bun database handle with the correct dialect, and performs an initial `PingContext`, failing fast on errors. Both expose typed `PoolConfig`/`TimeoutConfig` and an optional post-build hook for driver options not surfaced by the typed configs.
