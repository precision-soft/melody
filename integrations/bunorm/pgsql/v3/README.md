# Bun ORM PostgreSQL provider (v3)

PostgreSQL provider for the Melody v3 [`bunorm`](../../v3) integration. It implements [`bunorm.Provider`](../../v3/provider.go) and produces a Bun database handle configured with the PostgreSQL dialect.

Import path: `github.com/precision-soft/melody/integrations/bunorm/pgsql/v3`

## Provider

[`pgsql.NewProvider`](./provider.go) builds a [`pgsql.Provider`](./provider.go) from optional [`ProviderOption`](./provider_option.go) values. Connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v3/connection_parameters.go) the manager registry passes to `Open` — the provider itself holds only dialect/driver tuning.

```go
provider := pgsql.NewProvider()
```

Register it through the core registry by attaching it to a [`bunorm.ProviderDefinition`](../../v3/provider_definition.go) (see the [bunorm README](../../v3/README.md)).

Unlike the [MySQL provider](../../mysql/v3/README.md), this package ships no self-registering application module or registration helper — register the provider through the core registry as shown above. It does ship an application-level service: an advisory-lock [`Locker`](./lock.go) (see [Distributed lock](#distributed-lock) below).

### Options

* [`WithPoolConfig`](./provider_option.go) — connection-pool sizing via [`NewPoolConfig`](./pool_config.go).
* [`WithTimeoutConfig`](./provider_option.go) — the **connect timeout only** via [`NewTimeoutConfig`](./timeout_config.go). Unlike the MySQL provider, [`TimeoutConfig`](./timeout_config.go) here carries a single `ConnectTimeout` field: `pgdriver` exposes no separate read/write deadlines, so there are none to configure. A `ConnectTimeout` of `0` skips the bounded ping context (no artificial deadline on a healthy database).
* [`WithRetryConfig`](./provider_option.go) — connection retry/backoff via [`NewRetryConfig`](./retry_config.go). Retrying is **opt-in**: without this option `Open` makes a single attempt.
* [`WithPostBuildHook`](./provider_option.go) — advanced connector customization (see below).
* [`WithInsecure`](./provider_option.go) / [`WithTlsConfig`](./provider_option.go) — TLS controls (see below).

### Defaults

Applied when the matching option is not set ([`DefaultPoolConfig`](./pool_config.go), [`DefaultTimeoutConfig`](./timeout_config.go), [`DefaultRetryConfig`](./retry_config.go)):

| Config          | Field                   | Default              |
|-----------------|-------------------------|----------------------|
| `PoolConfig`    | `MaxOpenConnections`    | `50`                 |
| `PoolConfig`    | `MaxIdleConnections`    | `25`                 |
| `PoolConfig`    | `ConnectionMaxLifetime` | `5m`                 |
| `PoolConfig`    | `ConnectionMaxIdleTime` | `1m`                 |
| `TimeoutConfig` | `ConnectTimeout`        | `5s`                 |
| `RetryConfig`   | `MaxAttempts`           | `3`                  |
| `RetryConfig`   | `InitialDelay`          | `500ms`              |
| `RetryConfig`   | `MaxDelay`              | `5s`                 |
| `RetryConfig`   | `BackoffMultiplier`     | `2.0`                |

`RetryConfig` values are also individually defaulted: a non-positive `InitialDelay`/`MaxDelay` or a `BackoffMultiplier` that is not at least `1` (including `NaN`) falls back to the value above, so a partially-filled config cannot collapse the backoff into a re-dial storm.

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

## Distributed lock

[`pgsql.NewLocker(database, options...)`](./lock.go) returns a `lock/contract.Locker` backed by PostgreSQL session advisory locks (`pg_try_advisory_lock` / `pg_advisory_unlock`) — the Postgres counterpart of the [MySQL `GET_LOCK` locker](../../mysql/v3/README.md). Lock names are hashed with FNV-1a (64-bit) into the two 32-bit halves of the advisory key.

```go
locker := pgsql.NewLocker(database) // database is a *bun.DB

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

- **Try-acquire only.** `Acquire` issues `SELECT pg_try_advisory_lock($1, $2)` — a non-blocking attempt that returns immediately. A failed acquisition returns `(false, nil)`, not an error.
- **Session-pinned.** Each held lock pins a dedicated `*sql.Conn` for its lifetime; `Release` runs `pg_advisory_unlock` on that same connection — on a fresh context, bounded by [`WithLockReleaseTimeout`](./lock.go) (default 5s) — before returning it to the pool.
- **Reentrant within a `Lock`.** Calling `Acquire` on a lock that is already held verifies ownership and returns `(true, nil)`; if the lease was lost it re-acquires on a fresh connection.
- **No TTL.** Session advisory locks do not auto-expire — the `ttl` passed to `CreateLock` is accepted only for interface compatibility. The lock is released by `Release` or when its connection drops (e.g. the process dies). For TTL-based auto-expiry, use the Redis backend in [`integrations/rueidis/v3`](../../../rueidis).
- **`Refresh` is a liveness probe.** Because there is nothing to extend, `Refresh` instead pings the pinned connection (`PingContext`, on a fresh context bounded by the release timeout) rather than introspecting `pg_locks`: a session advisory lock is held for exactly as long as its backend session lives, so a connection that still answers still holds the lock. Only a genuinely dead session fails the refresh — its connection is then discarded and a "lock is no longer held" error returned, matching the lost-lock signal of the other backends. A transient cause such as a cancelled or expired request context can never be mistaken for a lost lock, because the probe does not use the caller's context.

Unlike the MySQL package there is no `RegisterLockerService` helper or module; register the locker under the core `lock.ServiceLocker` service name yourself if handlers should resolve it via `lock.LockerMustFromResolver`.

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
