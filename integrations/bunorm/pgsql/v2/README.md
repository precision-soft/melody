# Bun ORM PostgreSQL provider

PostgreSQL provider module for Bun ORM integration with Melody.

This module implements [`bunorm.Provider`](../provider.go) and produces a Bun database handle configured with the PostgreSQL dialect.

## Import

- `github.com/precision-soft/melody/integrations/bunorm/v2`
- `github.com/precision-soft/melody/integrations/bunorm/pgsql/v2`

## Provider

[`pgsql.NewProvider`](./provider.go) builds a [`pgsql.Provider`](./provider.go) from optional [`ProviderOption`](./provider_option.go) values. Connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParams`](../connection_parameters.go) passed to `Open` — the provider itself holds only dialect/driver tuning.

Pool and timeout defaults can be overridden via the chainable [`WithPoolConfig`](./provider.go) and [`WithTimeoutConfig`](./provider.go) methods using [`PoolConfig`](./pool_config.go) and [`TimeoutConfig`](./timeout_config.go).

## TLS

Starting with `integrations/bunorm/pgsql/v3.1.0` the provider is **secure-by-default**: `pgdriver` negotiates a TLS handshake on every Postgres connection. Earlier releases hardcoded `pgdriver.WithInsecure(true)`, which silently disabled TLS.

Two provider options expose the TLS knobs:

- [`pgsql.WithInsecure(bool)`](./provider_option.go) — default `false`. Pass `true` to restore the legacy plain-TCP behaviour (for local development or non-TLS endpoints).
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
			connector.Config().TLSConfig.InsecureSkipVerify = true
			return nil
		}),
	)
}
```
