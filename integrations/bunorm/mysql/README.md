# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts; [`mysql.RetryConfig`](./retry_config.go) controls the opt-in initial-connection retry.

## Defaults

`PoolConfig` and `TimeoutConfig` defaults apply when the matching config is not set ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go)). The `RetryConfig` rows are different: an absent `RetryConfig` means **no retry at all**, and the listed values fill in field by field when a `RetryConfig` is supplied with that field zero or non-positive — except `BackoffMultiplier`, whose floor is `1`: any supplied value below it, `NaN` included, falls back to the default, while exactly `1` stays a valid constant backoff ([`DefaultRetryConfig`](./retry_config.go) builds the same shape for callers who want it whole):

| Config          | Field                   | Default |
|-----------------|-------------------------|---------|
| `PoolConfig`    | `MaxOpenConnections`    | `25`    |
| `PoolConfig`    | `MaxIdleConnections`    | `5`     |
| `PoolConfig`    | `ConnectionMaxLifetime` | `5m`    |
| `PoolConfig`    | `ConnectionMaxIdleTime` | `1m`    |
| `TimeoutConfig` | `ConnectTimeout`        | `10s`   |
| `TimeoutConfig` | `ReadTimeout`           | `30s`   |
| `TimeoutConfig` | `WriteTimeout`          | `30s`   |
| `RetryConfig`   | `MaxAttempts`           | `3`     |
| `RetryConfig`   | `InitialDelay`          | `500ms` |
| `RetryConfig`   | `MaxDelay`              | `5s`    |
| `RetryConfig`   | `BackoffMultiplier`     | `2.0`   |

Retrying is **opt-in**: without a `RetryConfig`, `Open` makes a single attempt. Only transient failures are retried; a non-transient error fails immediately. `ConnectTimeout` also bounds the initial `PingContext` and the post-build hook; a non-positive value falls back to the 10s default before the connector is built, so the dial, ping and hook always run under a deadline.

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

## Where bun's own diagnostics go

Bun reports the developer's declaration mistakes — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — through a package-level logger of its own. Opening a connection through this provider routes that logger into the application's journal, once per process, so those arrive as `warning` records under the message `bun diagnostic` with the line in the context. Without it they are written to standard error as unstructured text, which a deployment whose journal is a json file never sees. The pgsql provider does the same; the first of the two to open wins the setting, and an application has one journal either way.

One line does **not** travel that way. When the mysql dialect cannot read the server version it writes

```
can't discover MySQL version: <error>
```

through the **standard library's** default logger, not through bun's, so nothing this provider sets can reach it. Routing it means `log.SetOutput`, which replaces the destination of the standard logger for the whole process — every dependency that logs through it, and your own `log` calls with it. That is the application's decision, not this package's, so if you want it, take it in your composition root:

```go
log.SetOutput(logging.NewStandardErrorLogger(logger, "standard logger").Writer())
```

In practice the line is redundant wherever container stderr is already collected into the same place as the journal, and the melody record written microseconds later carries strictly more: the level, the connection context and the cause.
