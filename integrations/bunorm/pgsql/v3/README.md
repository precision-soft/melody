# Bun ORM PostgreSQL provider (v3)

PostgreSQL provider for the Melody v3 [`bunorm`](../../v3) integration. It implements [`bunorm.Provider`](../../v3/provider.go) and produces a Bun database handle configured with the PostgreSQL dialect.

Import path: `github.com/precision-soft/melody/integrations/bunorm/pgsql/v3`

## Provider

[`pgsql.NewProvider`](./provider.go) builds a [`pgsql.Provider`](./provider.go) from optional [`ProviderOption`](./provider_option.go) values. Connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParams`](../../v3/connection_params.go) the manager registry passes to `Open` — the provider itself holds only dialect/driver tuning.

```go
provider := pgsql.NewProvider()
```

Register it through the core registry by attaching it to a [`bunorm.ProviderDefinition`](../../v3/provider_definition.go) (see the [bunorm README](../../v3/README.md)).

Unlike the [MySQL provider](../../mysql/v3/README.md), this package ships no self-registering application module: PostgreSQL exposes no application-level service (the MySQL module exists only to register the advisory-lock `Locker`). Register the provider through the core registry as shown above.

### Options

* [`WithPoolConfig`](./provider_option.go) — connection-pool sizing via [`NewPoolConfig`](./pool_config.go).
* [`WithTimeoutConfig`](./provider_option.go) — connect/read/write timeouts via [`NewTimeoutConfig`](./timeout_config.go). A `ConnectTimeout` of `0` skips the bounded ping context (no artificial deadline on a healthy database).
* [`WithRetryConfig`](./provider_option.go) — connection retry/backoff via [`NewRetryConfig`](./retry_config.go).
* [`WithPostBuildHook`](./provider_option.go) — advanced connector customization (see below).
* [`WithInsecure`](./provider_option.go) / [`WithTlsConfig`](./provider_option.go) — TLS controls (see below).

## TLS

The provider is **secure-by-default**: `pgdriver` negotiates a TLS handshake on every Postgres connection.

* [`pgsql.WithInsecure(bool)`](./provider_option.go) — default `false`. Pass `true` to use plain TCP (local development or non-TLS endpoints).
* [`pgsql.WithTlsConfig(*tls.Config)`](./provider_option.go) — forwards a caller-built `*crypto/tls.Config` to `pgdriver.WithTLSConfig(...)`. When set, it takes precedence over `WithInsecure(...)`.

```go
// Connect against a local Postgres that does not expose TLS:
provider := pgsql.NewProvider(pgsql.WithInsecure(true))
```

```go
// Force TLS with a pinned root CA:
rootCaPool := x509.NewCertPool()
rootCaPool.AppendCertsFromPEM(rootCaPem)

provider := pgsql.NewProvider(pgsql.WithTlsConfig(&tls.Config{
    ServerName: "db.example.com",
    RootCAs:    rootCaPool,
    MinVersion: tls.VersionTLS12,
}))
```

## Advanced connector customization

For driver options not exposed by the typed configs, use a post-build hook configured via [`pgsql.WithPostBuildHook`](./provider_option.go) with the [`pgsql.PostBuildHook`](./post_build_hook.go) signature. The hook runs during `Open`, after Melody defaults and typed configs are applied and before the SQL database is opened.

```go
provider := pgsql.NewProvider(
    pgsql.WithPostBuildHook(func(ctx context.Context, connector *pgdriver.Connector) error {
        connector.Config().TLSConfig.InsecureSkipVerify = true
        return nil
    }),
)
```
