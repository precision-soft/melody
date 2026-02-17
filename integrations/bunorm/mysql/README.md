# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts.

Notes

- Connection errors are returned as Melody exceptions with a safe context.
- This module does not register services by itself; service registration is left to the consuming application.

## Advanced connector customization

If you need driver options that are not exposed by [`mysql.TimeoutConfig`](./timeout_config.go) or other typed configs, use a post-build hook.

Provider constructors accept optional provider options:

- [`mysql.NewProvider`](./provider.go)
- [`mysql.NewProviderWithConfig`](./provider.go)

Configure a hook via [`mysql.WithPostBuildHook`](./provider_option.go) using the [`mysql.PostBuildHook`](./post_build_hook.go) signature.

The hook is executed during open, after Melody defaults and typed configs are applied, and before creating the SQL connector.

Example:

```go
provider := mysql.NewProvider(
    "DB_HOST",
    "DB_PORT",
    "DB_DATABASE",
    "DB_USER",
    "DB_PASSWORD",
    mysql.WithPostBuildHook(func(ctx context.Context, resolver containercontract.Resolver, driverConfig *driver.Config) error {
        _ = ctx
        _ = resolver
        driverConfig.TLSConfig = "custom"
        return nil
    }),
)
```
