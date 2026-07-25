# Bun ORM MySQL provider

This module provides a MySQL provider implementation for the generic Bun ORM integration.

Import paths

- Generic integration: `github.com/precision-soft/melody/integrations/bunorm/v2`
- MySQL provider: `github.com/precision-soft/melody/integrations/bunorm/mysql/v2`

Key types

- [`mysql.Provider`](./provider.go) implements [`bunorm.Provider`](../../v2/provider.go) and opens a Bun database handle using `go-sql-driver/mysql` + `mysqldialect`.
- [`mysql.PoolConfig`](./pool_config.go) and [`mysql.TimeoutConfig`](./timeout_config.go) control connection pool and timeouts; [`mysql.RetryConfig`](./retry_config.go) controls the opt-in initial-connection retry. All three are set through the chainable [`WithPoolConfig`](./provider.go) / [`WithTimeoutConfig`](./provider.go) / [`WithRetryConfig`](./provider.go) methods.

## Defaults

Applied when the matching config is not set ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go), [`DefaultRetryConfig`](./retry_config.go)):

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

Retrying is **opt-in**: without a `RetryConfig`, `Open` makes a single attempt. Only transient failures are retried; a non-transient error fails immediately. `ConnectTimeout` also bounds the initial `PingContext` and the post-build hook; a value of `0` leaves both unbounded.

Notes

- Connection errors are returned as Melody exceptions with a safe context.
- This module does not register services by itself; service registration is left to the consuming application.

## Advanced connector customization

If you need driver options that are not exposed by [`mysql.TimeoutConfig`](./timeout_config.go) or other typed configs, use a post-build hook.

The provider constructor accepts optional provider options:

- [`mysql.NewProvider`](./provider.go)

Configure a hook via [`mysql.WithPostBuildHook`](./provider_option.go) using the [`mysql.PostBuildHook`](./post_build_hook.go) signature.

The hook is executed during open, after Melody defaults and typed configs are applied, and before creating the SQL connector.

Example:

```go
provider := mysql.NewProvider(
mysql.WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
_ = ctx
driverConfig.TLSConfig = "custom"
return nil
}),
)
```
