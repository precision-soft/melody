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

The same module also ships the MySQL dialect [`Provider`](./provider.go) for the core [`bunorm`](../../v3) registry. [`mysql.NewProvider`](./provider.go) takes optional [`ProviderOption`](./provider_option.go) values; connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v3/connection_parameters.go) the manager registry passes to `Open`. Pool, timeout and retry tuning lives in [`mysql.PoolConfig`](./pool_config.go), [`mysql.TimeoutConfig`](./timeout_config.go) and [`mysql.RetryConfig`](./retry_config.go), applied through the [`WithPoolConfig`](./provider_option.go) / [`WithTimeoutConfig`](./provider_option.go) / [`WithRetryConfig`](./provider_option.go) options, and [`WithPostBuildHook`](./provider_option.go) reaches driver options the typed configs do not expose, through the [`mysql.PostBuildHook`](./post_build_hook.go) signature.

Because the provider is given values rather than the configuration keys they came from, arming the framework's credential redaction is the application's call: declare the parameter with the parameter registrar's `RegisterSecretParameter`, or mark one melody registered from the `.env` artifacts with `MarkParameterSecret`. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it.

### Defaults

All three configurations fill in **field by field**: a supplied `PoolConfig` or `TimeoutConfig` has every non-positive field replaced by the listed default, so passing `NewPoolConfig(0, 0, 0, 0)` yields the defaults rather than the zeros — on `database/sql` a zero maximum means *unlimited*, which is not a sizing anyone asks for by omission. What makes `RetryConfig` different is absence alone: an absent `RetryConfig` means **no retry at all** rather than the defaults, while a supplied one fills in field by field like the other two — except `BackoffMultiplier`, whose floor is `1`: any supplied value below it, `NaN` included, falls back to the default, while exactly `1` stays a valid constant backoff.

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

Retrying is **opt-in**: without a `RetryConfig`, `Open` makes a single attempt. Only transient failures are retried; a non-transient error fails immediately. `ConnectTimeout` also bounds the initial `PingContext` and the post-build hook; a non-positive value falls back to the 10s default before the connector is built, so the dial, ping and hook always run under a deadline.

The lock's own release/verify round trips are bounded separately by [`WithLockReleaseTimeout`](./lock.go) (default 5s).

### Opening under a context, and opening for migrations

- [`Provider.OpenContext`](./provider.go) implements [`bunorm.ContextOpener`](../../v3/provider.go): the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget. The registry prefers it and hands the context it was constructed with.
- [`Provider.OpenForMigration`](./provider.go) implements [`bunorm.MigrationProvider`](../../v3/provider.go) and opens the same database with the driver deadlines lifted: `ReadTimeout` and `WriteTimeout` are per-connection settings baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The connect timeout stays armed, the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run.
- [`Provider.OpenForMigrationContext`](./provider.go) implements [`bunorm.MigrationContextOpener`](../../v3/provider.go) — the migration open under the caller's context, the way `OpenContext` is `Open` under it.

### Transport security

The provider negotiates a **verified TLS handshake by default**: it builds a `tls.Config` from the system roots, verifies the server certificate against the configured host, and requires TLS 1.2 or higher. A server that speaks no TLS fails the dial rather than falling back to plaintext, and the driver's `skip-verify` spelling — TLS negotiated but the certificate never checked — is not used, because it is trivially machine-in-the-middled.

Two options shape it:

- [`mysql.WithInsecure(true)`](./provider_option.go) disables TLS entirely, leaving a plaintext connection. It is the deliberate opt-out for a database reached over a trusted network or one that speaks no TLS — the same option, spelled the same way, as the pgsql provider.
- [`mysql.WithTlsConfig`](./provider_option.go) hands the connector an explicit `*tls.Config` — a pinned server certificate, a client certificate — taking precedence over both the default and `WithInsecure`.

```go
provider := mysql.NewProvider(
    mysql.WithInsecure(true), // plaintext, for a trusted network or a server without TLS
)
```

### Where bun's own diagnostics go

Bun reports the developer's declaration mistakes — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — through a package-level logger of its own. Opening a connection through this provider routes that logger into the application's journal, once per process, so those arrive as `warning` records under the message `bun diagnostic` with the line in the context. Without it they are written to standard error as unstructured text, which a deployment whose journal is a json file never sees. The pgsql provider does the same, and the setting is taken once for the whole process: the first open wins it and every later one is ignored, which is harmless while both providers were handed the same journal. It is not harmless when the first open was handed **no** logger — a `nil` is resolved to a no-op logger before it reaches the routing, so that no-op wins the process and the diagnostics of every provider opened
afterwards are dropped silently. Hand the first provider that opens the journal you want bun's diagnostics in.

One line does **not** travel that way. When the mysql dialect cannot read the server version it writes

```
can't discover MySQL version: <error>
```

through the **standard library's** default logger, not through bun's, so nothing this provider sets can reach it. Routing it means `log.SetOutput`, which replaces the destination of the standard logger for the whole process — every dependency that logs through it, and your own `log` calls with it. That is the application's decision, not this package's, so if you want it, take it in your composition root:

```go
log.SetOutput(logging.NewStandardErrorLogger(logger, "standard logger").Writer())
```

In practice the line is redundant wherever container stderr is already collected into the same place as the journal, and the melody record written microseconds later carries strictly more: the level, the connection context and the cause.
