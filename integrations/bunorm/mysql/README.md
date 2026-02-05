# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql`

Key types

- `mysql.Provider` implements `bunorm.Provider` and opens `*bun.DB` using `go-sql-driver/mysql` + `mysqldialect`.
- `mysql.PoolConfig` and `mysql.TimeoutConfig` control connection pool and timeouts.

Notes

- Connection errors are returned as Melody exceptions with a safe context.
- This module does not register services by itself; service registration is left to the consuming application.
