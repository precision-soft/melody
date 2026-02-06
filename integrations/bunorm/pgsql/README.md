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
