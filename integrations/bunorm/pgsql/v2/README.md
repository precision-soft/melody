# Bun ORM PostgreSQL provider

PostgreSQL provider module for Bun ORM integration with Melody.

This module implements [`bunorm.Provider`](../../v2/provider.go) and produces a Bun database handle configured with the PostgreSQL dialect.

## Import

- `github.com/precision-soft/melody/integrations/bunorm/v2`
- `github.com/precision-soft/melody/integrations/bunorm/pgsql/v2`

## Provider

[`pgsql.NewProvider`](./provider.go) builds a [`pgsql.Provider`](./provider.go) from optional [`ProviderOption`](./provider_option.go) values. Connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v2/connection_parameters.go) the registry hands to `Open` — the provider itself holds only dialect, transport and driver tuning. Because the provider is given values rather than the configuration keys they came from, arming the framework's credential redaction is the application's call: name the parameters to [`bunorm.ManagerRegistry.MarkSecretParameters`](../../v2/manager_registry.go).

Pool, timeout and retry defaults can be overridden via the chainable [`WithPoolConfig`](./provider.go), [`WithTimeoutConfig`](./provider.go) and [`WithRetryConfig`](./provider.go) methods using [`PoolConfig`](./pool_config.go), [`TimeoutConfig`](./timeout_config.go) and [`RetryConfig`](./retry_config.go). [`TimeoutConfig`](./timeout_config.go) names every deadline the driver applies — [`NewTimeoutConfig`](./timeout_config.go) takes the connect, read and write timeouts — because without explicit read and write deadlines `pgdriver` applies its own defaults, 10 seconds per read and 5 per write, which cut long statements with nothing in this configuration to mention they exist. For statements that must outlive even the configured deadlines, [`OpenForMigration`](./provider.go) opens a dedicated connection with the read and write deadlines lifted, which is what the migration commands run on.

### Defaults

All three configurations fill in **field by field**: a supplied `PoolConfig` or `TimeoutConfig` has every non-positive field replaced by the listed default, so passing `NewPoolConfig(0, 0, 0, 0)` yields the defaults rather than the zeros — on `database/sql` a zero maximum means *unlimited*, which is not a sizing anyone asks for by omission. An absent `PoolConfig` or `TimeoutConfig` is the whole default ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go)). What makes `RetryConfig` different is absence alone: an absent `RetryConfig` means **no retry at all** rather than the defaults, while a supplied one fills in field by field like the other two — except `BackoffMultiplier`, whose floor is `1`: any supplied value below it, `NaN` included, falls back to the default, while exactly `1` stays a valid constant backoff ([`DefaultRetryConfig`](./retry_config.go) builds the same shape for callers who want it whole):

| Config          | Field                   | Default |
|-----------------|-------------------------|---------|
| `PoolConfig`    | `MaxOpenConnections`    | `50`    |
| `PoolConfig`    | `MaxIdleConnections`    | `25`    |
| `PoolConfig`    | `ConnectionMaxLifetime` | `5m`    |
| `PoolConfig`    | `ConnectionMaxIdleTime` | `1m`    |
| `TimeoutConfig` | `ConnectTimeout`        | `5s`    |
| `TimeoutConfig` | `ReadTimeout`           | `30s`   |
| `TimeoutConfig` | `WriteTimeout`          | `30s`   |
| `RetryConfig`   | `MaxAttempts`           | `3`     |
| `RetryConfig`   | `InitialDelay`          | `500ms` |
| `RetryConfig`   | `MaxDelay`              | `5s`    |
| `RetryConfig`   | `BackoffMultiplier`     | `2.0`   |

Retrying is **opt-in**: without a `RetryConfig`, `Open` makes a single attempt.

## Opening under a context, and opening for migrations

- [`Provider.OpenContext`](./provider.go) implements [`bunorm.ContextOpener`](../../v2/provider.go): the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget. The registry prefers it and hands the context it was constructed with.
- [`Provider.OpenForMigration`](./provider.go) implements [`bunorm.MigrationProvider`](../../v2/provider.go) and opens the same database with the read and write deadlines lifted, the connect timeout still armed, over a pool of the two connections a sequential migration run needs and with no connection recycled mid-run.
- [`Provider.OpenForMigrationContext`](./provider.go) implements [`bunorm.MigrationContextOpener`](../../v2/provider.go) — the migration open under the caller's context, the way `OpenContext` is `Open` under it.

## TLS

Starting with `integrations/bunorm/pgsql v2.0.4` the provider is **secure-by-default**: `pgdriver` negotiates a verified TLS handshake on every Postgres connection. Earlier releases of this module called `pgdriver.WithInsecure(false)`, which — despite the name — negotiated TLS with `InsecureSkipVerify: true`, so the server certificate was never checked.

Two provider options expose the TLS knobs:

- [`pgsql.WithInsecure(bool)`](./provider_option.go) — default `false`. Pass `true` to disable TLS entirely and connect over plain TCP (for local development or non-TLS endpoints). It does not *restore* an earlier default: before the verifying handshake landed, this provider connected through `pgdriver`'s own insecure mode, which despite its name negotiates TLS with `InsecureSkipVerify: true` — encrypted but unauthenticated, never plain TCP. `true` here is a deliberate step further out than that.
- [`pgsql.WithTlsConfig(*tls.Config)`](./provider_option.go) — forwards a caller-built `*crypto/tls.Config` to `pgdriver.WithTLSConfig(...)`. When set, it takes precedence over `WithInsecure(...)`.

Example — connect against a local Postgres that does not expose TLS:

```go
provider := pgsql.NewProvider(pgsql.WithInsecure(true))
```

Example — force TLS with a pinned root CA:

```go
rootCaPool := x509.NewCertPool()
rootCaPool.AppendCertsFromPEM(rootCaPem)

provider := pgsql.NewProvider(pgsql.WithTlsConfig(&tls.Config{
    ServerName: "db.example.com",
    RootCAs:    rootCaPool,
    MinVersion: tls.VersionTLS12,
}))
```

## Advanced connector customization

If you need driver options that are not exposed by [`TimeoutConfig`](./timeout_config.go) or other typed configs, use a post-build hook.

The provider constructor accepts optional provider options:

- [`pgsql.NewProvider`](./provider.go)

Configure a hook via [`pgsql.WithPostBuildHook`](./provider_option.go) using the [`pgsql.PostBuildHook`](./post_build_hook.go) signature.

The hook is executed during open, after Melody defaults and typed configs are applied, and before opening the SQL database.

Example:

```go
package main

func main() {
    provider := pgsql.NewProvider(
        pgsql.WithPostBuildHook(func(ctx context.Context, connector *pgdriver.Connector) error {
            _ = ctx

            /* WithInsecure(true) leaves TLSConfig nil, so the hook must not assume one is there */
            tlsConfig := connector.Config().TLSConfig
            if nil == tlsConfig {
                return nil
            }

            tlsConfig.InsecureSkipVerify = true

            return nil
        }),
    )
}
```
