# Melody MySQL lock integration (v3)

A MySQL-backed implementation of the Melody core [`lock`](https://github.com/precision-soft/melody) contract, built on MySQL advisory locks (`GET_LOCK` / `RELEASE_LOCK`). It is a drop-in `lock/contract.Locker` for applications that already run a MySQL database through [`bunorm`](../../v3) and want a distributed named lock without standing up Redis.

Import path: `github.com/precision-soft/melody/integrations/bunorm/mysql/v3`

## Usage

```go
import (
    "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    "github.com/precision-soft/melody/v3/lock"
)

locker := mysql.NewLocker(database) // database is a *bun.DB

namedLock := locker.CreateLock("import:catalog", 30*time.Second)

acquired, err := namedLock.Acquire(runtime)
if err != nil {
    // connection or query error
}
if !acquired {
    // another holder owns the lock — do not proceed
    return
}
defer namedLock.Release(runtime)
```

`NewLocker(database)` takes the `*bun.DB` handle and implements `lock/contract.Locker`; `CreateLock(name, ttl)` returns a `lock/contract.Lock`.

### Plug-and-play registration

Register the locker under the core `lock.ServiceLocker` service name in one call, so handlers resolve it with `lock.LockerMustFromResolver`:

```go
mysql.RegisterLockerService(registrar, database)
```

Or bundle it as a self-registering application module — one `RegisterModule` call registers the locker (opt-in via `AsLocker`, skipped when the database is nil):

```go
app.RegisterModule(mysql.NewModule(mysql.ModuleConfig{Database: database, AsLocker: true}))
```

## Semantics

- **Try-acquire only.** `Acquire` issues `SELECT GET_LOCK(?, 0)` — a non-blocking attempt that returns immediately, consistent with the in-memory and Redis backends. A failed acquisition returns `(false, nil)`, not an error.
- **Session-pinned.** Each held lock owns a dedicated `*sql.Conn` that is pinned for the lifetime of the lock. MySQL advisory locks are scoped to the connection that took them, so `Release` (`DO RELEASE_LOCK(?)`) and the ownership check run on that same connection, which is then closed and returned to the pool.
- **Reentrant within a `Lock`.** Calling `Acquire` again on a lock that is already held re-verifies ownership first — it issues `SELECT IS_USED_LOCK(?) = CONNECTION_ID()` on the pinned connection (on a fresh, bounded context, so a cancelled request context cannot look like a lost lock) and returns `(true, nil)` when it still holds the lock. If that verification fails — the pinned connection was dropped, so MySQL already released the lock — the pinned connection is released and closed and a fresh one re-acquires, instead of falsely reporting the lost lock as still held.
- **No TTL.** MySQL advisory locks are connection-lifetime: they do not auto-expire. The `ttl` passed to `CreateLock` is accepted only for interface compatibility and is **not** honored as an expiry — the lock is released by `Release` or when its connection drops (e.g. the process dies). For TTL-based auto-expiry, use the Redis backend in [`integrations/rueidis/v3`](../../../rueidis).
- **`Refresh` verifies ownership.** Because there is nothing to extend, `Refresh` instead confirms the lock is still held on its connection via `SELECT IS_USED_LOCK(?) = CONNECTION_ID()` and returns a "lock is no longer held" error if the lease was lost — matching the lost-lock signal of the in-memory and Redis backends.

See the core [`LOCK.md`](../../../../v3/.documentation/package/LOCK.md) package documentation for the contract and backend comparison.

## Provider

The same module also ships the MySQL dialect [`Provider`](./provider.go) for the core [`bunorm`](../../v3) registry. [`mysql.NewProvider`](./provider.go) takes optional [`ProviderOption`](./provider_option.go) values; connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v3/connection_parameters.go) the manager registry passes to `Open`. Pool, timeout and retry tuning is applied through the [`WithPoolConfig`](./provider_option.go) / [`WithTimeoutConfig`](./provider_option.go) / [`WithRetryConfig`](./provider_option.go) options, and [`WithPostBuildHook`](./provider_option.go) reaches driver options the typed configs do not expose.

### Defaults

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

The lock's own release/verify round trips are bounded separately by [`WithLockReleaseTimeout`](./lock.go) (default 5s).
