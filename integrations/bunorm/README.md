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
go get github.com/precision-soft/melody/integrations/bunorm@latest
```

### MySQL provider

```bash
go get github.com/precision-soft/melody/integrations/bunorm/mysql@latest
```

### PostgreSQL provider

```bash
go get github.com/precision-soft/melody/integrations/bunorm/pgsql@latest
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

	"github.com/precision-soft/melody/application"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"

	"github.com/precision-soft/melody/integrations/bunorm"
	bunmysql "github.com/precision-soft/melody/integrations/bunorm/mysql"
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
	"github.com/precision-soft/melody/integrations/bunorm"
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

## Enhancements

The core `bunorm` module ships three optional, dependency-free enhancements (standard library + Bun only). Paths below reference the v3 binding.

### Read/write split

[`ReadWriteSplitter`](v3/split.go) routes reads to replica managers and writes to the primary manager, resolving named managers from a [`ManagerRegistry`](v3/manager_registry.go). It is explicit: call `Writer()` for writes and `Reader()` for reads (replicas are chosen round-robin; with no replicas, the reader falls back to the primary).

```go
splitter := bunorm.NewReadWriteSplitter(registry, "primary", "replica-a", "replica-b")

writeDatabase, _ := splitter.Writer()
readDatabase, _ := splitter.Reader()
```

### Field encryption

The [`encrypt`](v3/encrypt) subpackage encrypts column values at rest with AES-256-GCM. Declare a field as [`encrypt.EncryptedString`](v3/encrypt/encrypted_string.go) (it implements `driver.Valuer`/`sql.Scanner`) and configure a process-wide cipher once at boot via [`encrypt.UseCipher`](v3/encrypt/encrypted_string.go). Keys are resolved by id through a [`KeyProvider`](v3/encrypt/key_provider.go), so ciphertext carries its key id for rotation.

```go
encrypt.UseCipher(encrypt.NewCipher(
	encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": key32}),
))

type Customer struct {
	Email encrypt.EncryptedString `bun:"email,type:varchar(255)"`
}
```

The value is stored as `<keyId>:<base64(nonce||ciphertext)>` and is not searchable in encrypted form. `EncryptedString` masks its decrypted plaintext in `fmt`/`slog`/error output (its `String`/`LogValue` return `<redacted>`); use an explicit `string(value)` conversion when the real value is needed.

### Audit trail

The [`audit`](v3/audit) subpackage records a **per-field before/after change-set** for entity writes into a separate audit database via a [`Recorder`](v3/audit/recorder.go). Recording is explicit (called from the repository/service layer with the `before`/`after` entity values): Bun has no Doctrine-style unit-of-work, so the original row is not available to a transparent hook — passing both states is the robust, exact approach.

```go
recorder := audit.NewRecorder(auditDatabase, "melody_audit")

ctx := audit.WithActor(requestContext, "user-42")

recorder.RecordInsert(ctx, "order", order.Id, order)
recorder.RecordUpdate(ctx, "order", order.Id, before, after)
recorder.RecordDelete(ctx, "order", order.Id, before)
```

[`ChangeSet`](v3/audit/change.go) diffs two struct values field by field (using `bun` column names, skipping relations and the embedded base model): `INSERT` records new values, `DELETE` records old values, `UPDATE` records only the changed fields as `{field, old, new}`. Each [`audit.Entry`](v3/audit/entry.go) stores the entity name, entity id, operation, the change-set as JSON, the actor (from context), and a timestamp — in a distinct audit `*bun.DB`.

Sensitive fields are recorded as changed but with their values masked to `<redacted>`: tag a field with `audit:"redact"`, or use an [`encrypt.EncryptedString`](v3/encrypt/encrypted_string.go) field (auto-redacted). A field tagged `bun:"-"` is excluded from the change-set entirely.
