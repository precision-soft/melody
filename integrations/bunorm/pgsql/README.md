# bunorm/pgsql

PostgreSQL provider module for Bun ORM integration with Melody.

This module implements `bunorm.Provider` and produces a `*bun.DB` configured with the PostgreSQL dialect.

## Import

- `github.com/precision-soft/melody/integrations/bunorm`
- `github.com/precision-soft/melody/integrations/bunorm/pgsql`

## Provider

`pgsql.Provider` reads configuration values from Melody config using the parameter names passed to `NewProvider(...)`.

Common parameter names:

- `DB_HOST`
- `DB_PORT`
- `DB_DATABASE`
- `DB_USER`
- `DB_PASSWORD`

Pool and timeout defaults can be overridden via `WithPoolConfig(...)` and `WithTimeoutConfig(...)`.
