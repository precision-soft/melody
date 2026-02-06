# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts.

Notes

- Connection errors are returned as Melody exceptions with a safe context.
- This module does not register services by itself; service registration is left to the consuming application.
