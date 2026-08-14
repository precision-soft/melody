# Bun ORM PostgreSQL provider

PostgreSQL provider module for Bun ORM integration with Melody.

This module implements [`bunorm.Provider`](../provider.go) and produces a Bun database handle configured with the PostgreSQL dialect.

## Import

- `github.com/precision-soft/melody/integrations/bunorm`
- `github.com/precision-soft/melody/integrations/bunorm/pgsql`

## Provider

[`pgsql.Provider`](./provider.go) reads configuration values from Melody config using the parameter names passed to [`NewProvider`](./provider.go).

Common parameter names:

- `DB_HOST`
- `DB_PORT`
- `DB_DATABASE`
- `DB_USER`
- `DB_PASSWORD`

Pool, timeout and retry defaults can be overridden via the chainable [`WithPoolConfig`](./provider.go), [`WithTimeoutConfig`](./provider.go) and [`WithRetryConfig`](./provider.go) methods (or up front through [`NewProviderWithConfig`](./provider.go)) using [`PoolConfig`](./pool_config.go), [`TimeoutConfig`](./timeout_config.go) and [`RetryConfig`](./retry_config.go). [`TimeoutConfig`](./timeout_config.go) names every deadline the driver applies — [`NewTimeoutConfig`](./timeout_config.go) takes the connect, read and write timeouts — because without explicit read and write deadlines `pgdriver` applies its own defaults, 10 seconds per read and 5 per write, which cut long statements with nothing in this configuration to mention they exist. For statements that must outlive even the configured deadlines, [`OpenForMigration`](./provider.go) opens a dedicated connection with the read and write deadlines lifted, which is what the migration commands run on.

### Defaults

`PoolConfig` and `TimeoutConfig` defaults apply when the matching config is not set ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go)). The `RetryConfig` rows are different: an absent `RetryConfig` means **no retry at all**, and the listed values fill in field by field when a `RetryConfig` is supplied with that field zero or non-positive ([`DefaultRetryConfig`](./retry_config.go) builds the same shape for callers who want it whole):

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

## TLS

Starting with `integrations/bunorm/pgsql v1.1.4` the provider is **secure-by-default**: `pgdriver` negotiates a verified TLS handshake on every Postgres connection. Earlier releases of this module called `pgdriver.WithInsecure(false)`, which — despite the name — negotiated TLS with `InsecureSkipVerify: true`, so the server certificate was never checked.

Two provider options expose the TLS knobs:

- [`pgsql.WithInsecure(bool)`](./provider_option.go) — default `false`. Pass `true` to restore the legacy plain-TCP behaviour (for local development or non-TLS endpoints).
- [`pgsql.WithTlsConfig(*tls.Config)`](./provider_option.go) — forwards a caller-built `*crypto/tls.Config` to `pgdriver.WithTLSConfig(...)`. When set, it takes precedence over `WithInsecure(...)`.

Example — connect against a local Postgres that does not expose TLS:

```go
provider := pgsql.NewProvider(
    "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USER", "DB_PASSWORD",
    pgsql.WithInsecure(true),
)
```

Example — force TLS with a pinned root CA:

```go
rootCaPool := x509.NewCertPool()
rootCaPool.AppendCertsFromPEM(rootCaPem)

provider := pgsql.NewProvider(
    "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USER", "DB_PASSWORD",
    pgsql.WithTlsConfig(&tls.Config{
        ServerName: "db.example.com",
        RootCAs:    rootCaPool,
        MinVersion: tls.VersionTLS12,
    }),
)
```

## Advanced connector customization

If you need driver options that are not exposed by [`TimeoutConfig`](./timeout_config.go) or other typed configs, use a post-build hook.

Provider constructors accept optional provider options:

- [`pgsql.NewProvider`](./provider.go)
- [`pgsql.NewProviderWithConfig`](./provider.go)

Configure a hook via [`pgsql.WithPostBuildHook`](./provider_option.go) using the [`pgsql.PostBuildHook`](./post_build_hook.go) signature.

The hook is executed during open, after Melody defaults and typed configs are applied, and before opening the SQL database.

Example:

```go
package main

func main() {
	provider := pgsql.NewProvider(
		"DB_HOST",
		"DB_PORT",
		"DB_DATABASE",
		"DB_USER",
		"DB_PASSWORD",
		pgsql.WithPostBuildHook(func(ctx context.Context, resolver containercontract.Resolver, connector *pgdriver.Connector) error {
			_ = ctx
			_ = resolver
			connector.Config().TLSConfig.InsecureSkipVerify = true
			return nil
		}),
	)
}
```
