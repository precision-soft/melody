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

Pool and timeout defaults can be overridden via [`WithPoolConfig`](./provider.go) and [`WithTimeoutConfig`](./provider.go) using [`PoolConfig`](./pool_config.go) and [`TimeoutConfig`](./timeout_config.go).

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
