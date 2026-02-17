# Bun ORM integration

This directory contains **optional Bun ORM integrations** for Melody.

The integration is split into independent Go modules so consumers can depend only on what they need:

* Core (dialect-agnostic): [`./`](./)
* MySQL provider: [`./mysql/`](./mysql/)
* PostgreSQL provider: [`./pgsql/`](./pgsql/)

## What you get

* A dialect-agnostic **manager registry** ([`bunorm.ManagerRegistry`](./manager_registry.go)) that:
    * Caches managers **1:1** per provider definition ([`bunorm.ProviderDefinition`](./provider_definition.go)).
    * Supports **exactly one default** provider (error if multiple defaults).
    * Falls back to the **first** provider as default if none is marked.

* A [`bunorm.Manager`](./manager.go) that owns a single Bun database handle and exposes:
    * `Database() *bun.DB`
    * `Close() error`

## Install

### Core

```bash
go get github.com/precision-soft/melody/integrations/bunorm/v2@latest
```

### MySQL provider

```bash
go get github.com/precision-soft/melody/integrations/bunorm/mysql/v2@latest
```

### PostgreSQL provider

```bash
go get github.com/precision-soft/melody/integrations/bunorm/pgsql/v2@latest
```

## Usage

The pattern is:

1. Register a [`*bunorm.ManagerRegistry`](./manager_registry.go) service (explicit id).
2. Register **only** the default [`*bunorm.Manager`](./manager.go) as a service that can be autowired by type.
3. Consume [`*bunorm.Manager`](./manager.go) (default) in your services/handlers.
4. Optionally, resolve the registry and request a named manager when you need a non-default database.

### Service registration example

```go
package main

import (
	"time"

	"github.com/precision-soft/melody/v2/application"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"

	bunmysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v2"
	"github.com/precision-soft/melody/integrations/bunorm/v2"
)

const (
	ServiceManagerRegistryId = "service.database.manager.registry"
	ServiceManagerDefaultId  = "service.database.manager.default"

	ManagerPrimaryName = "primary"
	ManagerAdminName   = "admin"
)

func RegisterDatabaseServices(app *application.Application) {
	app.RegisterService(
		ServiceManagerRegistryId,
		func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
			providers := []bunorm.ProviderDefinition{
				{
					Name: ManagerPrimaryName,
					Provider: bunmysql.NewProvider(
						"DB_HOST",
						"DB_PORT",
						"DB_DATABASE",
						"DB_USER",
						"DB_PASSWORD",
					).
						WithPoolConfig(
							bunmysql.NewPoolConfig(
								25,
								5,
								300*time.Second,
								60*time.Second,
							),
						).
						WithTimeoutConfig(
							bunmysql.NewTimeoutConfig(
								10*time.Second,
								30*time.Second,
								30*time.Second,
							),
						),
					IsDefault: true,
				},
				{
					Name: ManagerAdminName,
					Provider: bunmysql.NewProvider(
						"ADMIN_DB_HOST",
						"ADMIN_DB_PORT",
						"ADMIN_DB_DATABASE",
						"ADMIN_DB_USER",
						"ADMIN_DB_PASSWORD",
					),
				},
			}

			return bunorm.NewManagerRegistry(
				resolver,
				providers...,
			)
		},
	)

	app.RegisterService(
		ServiceManagerDefaultId,
		func(resolver containercontract.Resolver) (*bunorm.Manager, error) {
			registry := container.MustFromResolver[*bunorm.ManagerRegistry](
				resolver,
				ServiceManagerRegistryId,
			)

			return registry.DefaultManager()
		},
	)
}
```

### Consuming the default database

```go
package main

import (
	"github.com/precision-soft/melody/integrations/bunorm/v2"
)

type ApiService struct {
	databaseManager *bunorm.Manager
}

func NewApiService(databaseManager *bunorm.Manager) *ApiService {
	return &ApiService{
		databaseManager: databaseManager,
	}
}

func (instance *ApiService) Database() {
	database := instance.databaseManager.Database()
	_ = database
}
```

### Consuming a non-default database

```go
package main

func main() {
	registry := container.MustFromResolver[*bunorm.ManagerRegistry](resolver, ServiceManagerRegistryId)
	adminManager := registry.MustManager(ManagerAdminName)
	adminDatabase := adminManager.Database()
	_ = adminDatabase
}
```

## Dialect providers

* MySQL provider: [`./mysql/`](./mysql/)
* PostgreSQL provider: [`./pgsql/`](./pgsql/)

Each dialect module implements [`bunorm.Provider`](./provider.go) and is responsible for:

* Reading connection parameters.
* Building the driver connector.
* Constructing a Bun database handle with the correct dialect.
* Performing an initial `PingContext` and failing fast on errors.

## Advanced connector customization

The dialect providers expose an optional *post-build hook* that allows userland to alter driver configuration beyond what the provider exposes via typed config structs.

The hook is configured via a provider option passed to the provider constructor:

* MySQL: [`mysql.WithPostBuildHook`](./mysql/provider_option.go) using [`mysql.PostBuildHook`](./mysql/post_build_hook.go)
* PostgreSQL: [`pgsql.WithPostBuildHook`](./pgsql/provider_option.go) using [`pgsql.PostBuildHook`](./pgsql/post_build_hook.go)

The hook is executed during provider open, after Melody defaults and typed configs are applied, and before establishing the SQL connection.
