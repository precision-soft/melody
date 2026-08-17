# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts; [`mysql.RetryConfig`](./retry_config.go) controls the opt-in initial-connection retry.

## Defaults

All three configurations fill in **field by field**: a supplied `PoolConfig` or `TimeoutConfig` has every non-positive field replaced by the listed default, so passing `NewPoolConfig(0, 0, 0, 0)` yields the defaults rather than the zeros — on `database/sql` a zero maximum means *unlimited*, which is not a sizing anyone asks for by omission. An absent `PoolConfig` or `TimeoutConfig` is the whole default ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go)). What makes `RetryConfig` different is absence alone: an absent `RetryConfig` means **no retry at all** rather than the defaults, while a supplied one fills in field by field like the other two — except `BackoffMultiplier`, whose floor is `1`: any supplied value below it, `NaN` included, falls back to the default, while exactly `1` stays a valid constant backoff ([`DefaultRetryConfig`](./retry_config.go) builds the same shape for callers who want it whole):

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

## Transport security

The provider negotiates a **verified TLS handshake by default**: it builds a `tls.Config` from the system roots, verifies the server certificate against the configured host, and requires TLS 1.2 or higher. A server that speaks no TLS fails the dial rather than falling back to plaintext, and the driver's `skip-verify` spelling — TLS negotiated but the certificate never checked — is not used, because it is trivially machine-in-the-middled.

Two options shape it:

- [`mysql.WithInsecure(true)`](./provider_option.go) disables TLS entirely, leaving a plaintext connection. It is the deliberate opt-out for a database reached over a trusted network or one that speaks no TLS — the same option, spelled the same way, as the pgsql provider.
- [`mysql.WithTlsConfig`](./provider_option.go) hands the connector an explicit `*tls.Config` — a pinned server certificate, a client certificate — taking precedence over both the default and `WithInsecure`.

```go
provider := mysql.NewProvider(
    "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USER", "DB_PASSWORD",
    mysql.WithInsecure(true), // plaintext, for a trusted network or a server without TLS
)
```

## Advanced connector customization

If you need driver options that are not exposed by [`mysql.TimeoutConfig`](./timeout_config.go), the TLS options above, or other typed configs, use a post-build hook.

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
        driverConfig.Collation = "utf8mb4_unicode_ci"
        return nil
    }),
)
```

## Where bun's own diagnostics go

Bun reports the developer's declaration mistakes — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — through a package-level logger of its own. Opening a connection through this provider routes that logger into the application's journal, once per process, so those arrive as `warning` records under the message `bun diagnostic` with the line in the context. Without it they are written to standard error as unstructured text, which a deployment whose journal is a json file never sees. The pgsql provider does the same, and the setting is taken once for the whole process: the first open wins it and every later one is ignored, which is harmless while both providers were handed the same journal. It is not harmless when the first open was handed **no** logger — a `nil` is resolved to a no-op logger before it reaches the routing, so that no-op wins the process and the diagnostics of every provider opened afterwards are dropped silently. Hand the first provider that opens the journal you want bun's diagnostics in.

One line does **not** travel that way. When the mysql dialect cannot read the server version it writes

```
can't discover MySQL version: <error>
```

through the **standard library's** default logger, not through bun's, so nothing this provider sets can reach it. Routing it means `log.SetOutput`, which replaces the destination of the standard logger for the whole process — every dependency that logs through it, and your own `log` calls with it. That is the application's decision, not this package's, so if you want it, take it in your composition root:

```go
log.SetOutput(logging.NewStandardErrorLogger(logger, "standard logger").Writer())
```

In practice the line is redundant wherever container stderr is already collected into the same place as the journal, and the melody record written microseconds later carries strictly more: the level, the connection context and the cause.
