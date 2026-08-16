# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm/v2`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql/v2`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../../v2/provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts; [`mysql.RetryConfig`](./retry_config.go) controls the opt-in initial-connection retry. All three are set through the chainable [`WithPoolConfig`](./provider.go) / [`WithTimeoutConfig`](./provider.go) / [`WithRetryConfig`](./provider.go) methods.

Connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v2/connection_parameters.go) the registry hands to `Open` — the provider itself holds only dialect, transport and driver tuning. Because the provider is given values rather than the configuration keys they came from, arming the framework's credential redaction is the application's call: name the parameters to [`bunorm.ManagerRegistry.MarkSecretParameters`](../../v2/manager_registry.go).

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

## Opening under a context, and opening for migrations

- [`Provider.OpenContext`](./provider.go) implements [`bunorm.ContextOpener`](../../v2/provider.go): the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget. The registry prefers it and hands the context it was constructed with.
- [`Provider.OpenForMigration`](./provider.go) implements [`bunorm.MigrationProvider`](../../v2/provider.go) and opens the same database with the driver deadlines lifted: `ReadTimeout` and `WriteTimeout` are per-connection settings baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The connect timeout stays armed, the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run.
- [`Provider.OpenForMigrationContext`](./provider.go) implements [`bunorm.MigrationContextOpener`](../../v2/provider.go) — the migration open under the caller's context, the way `OpenContext` is `Open` under it.

## Transport security

The provider negotiates a **verified TLS handshake by default**: it builds a `tls.Config` from the system roots, verifies the server certificate against the configured host, and requires TLS 1.2 or higher. A server that speaks no TLS fails the dial rather than falling back to plaintext, and the driver's `skip-verify` spelling — TLS negotiated but the certificate never checked — is not used, because it is trivially machine-in-the-middled.

Two options shape it:

- [`mysql.WithInsecure(true)`](./provider_option.go) disables TLS entirely, leaving a plaintext connection. It is the deliberate opt-out for a database reached over a trusted network or one that speaks no TLS — the same option, spelled the same way, as the pgsql provider.
- [`mysql.WithTlsConfig`](./provider_option.go) hands the connector an explicit `*tls.Config` — a pinned server certificate, a client certificate — taking precedence over both the default and `WithInsecure`.

```go
provider := mysql.NewProvider(
    mysql.WithInsecure(true), // plaintext, for a trusted network or a server without TLS
)
```

## Advanced connector customization

If you need driver options that are not exposed by [`mysql.TimeoutConfig`](./timeout_config.go), the TLS options above, or other typed configs, use a post-build hook.

The provider constructor accepts optional provider options:

- [`mysql.NewProvider`](./provider.go)

Configure a hook via [`mysql.WithPostBuildHook`](./provider_option.go) using the [`mysql.PostBuildHook`](./post_build_hook.go) signature.

The hook is executed during open, after Melody defaults and typed configs are applied, and before creating the SQL connector.

Example:

```go
provider := mysql.NewProvider(
    mysql.WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
        _ = ctx
        driverConfig.Collation = "utf8mb4_unicode_ci"
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
