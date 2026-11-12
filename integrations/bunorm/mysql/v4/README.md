# Melody MySQL lock integration (v4)

A MySQL-backed implementation of the Melody core [`lock`](https://github.com/precision-soft/melody) contract, built on MySQL advisory locks (`GET_LOCK` / `RELEASE_LOCK`). It is a drop-in `lock/contract.Locker` for applications that already run a MySQL database through [`bunorm`](../../v4) and want a distributed named lock without standing up Redis.

Import path: `github.com/precision-soft/melody/integrations/bunorm/mysql/v4`

## Usage

```go
import (
    "github.com/precision-soft/melody/integrations/bunorm/mysql/v4"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/lock"
)

locker := mysql.NewLocker(database, mysql.LockerConfig{}) // database is a *bun.DB

namedLock := locker.CreateLock("import:catalog", bound.UnlimitedDuration())

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

`NewLocker(database, config)` takes the `*bun.DB` handle and a [`LockerConfig`](./lock.go), and implements `lock/contract.Locker`; `CreateLock(name, ttl)` returns a `lock/contract.Lock`. A nil database and a `ReleaseTimeout` outside its rule panic, the latter naming the field.

### Plug-and-play registration

Register the locker under the core `lock.ServiceLocker` service name in one call, so handlers resolve it with `lock.LockerMustFromResolver`:

```go
mysql.RegisterLockerService(registrar, database, mysql.LockerConfig{})
```

The configuration is resolved at the call, so a refused `ReleaseTimeout` panics at the wiring, not at the first lock. Or bundle it as a self-registering application module — one `RegisterModule` call registers the locker (opt-in via `AsLocker`, refused at boot when the database is nil), configured by `ModuleConfig.Locker`:

```go
app.RegisterModule(mysql.NewModule(mysql.ModuleConfig{Database: database, AsLocker: true}))
```

## Semantics

- **Try-acquire only.** `Acquire` issues `SELECT GET_LOCK(?, 0)` — a non-blocking attempt that returns immediately, consistent with the in-memory and Redis backends. A failed acquisition returns `(false, nil)`, not an error.
- **Names fold by case.** MySQL compares user-level lock names without case, so two names that differ only by letter case are one lock on this backend and two on the pgsql and rueidis ones; name locks in one case.
- **Session-pinned.** Each held lock owns a dedicated `*sql.Conn` that is pinned for the lifetime of the lock. MySQL advisory locks are scoped to the connection that took them, so `Release` (`DO RELEASE_LOCK(?)`) and the ownership check run on that same connection, which is then closed and returned to the pool.
- **Reentrant within a `Lock`.** Calling `Acquire` again on a lock that is already held re-verifies ownership first — it issues `SELECT IS_USED_LOCK(?) = CONNECTION_ID()` on the pinned connection (on a fresh, bounded context, so a cancelled request context cannot look like a lost lock) and returns `(true, nil)` when it still holds the lock. If the session answers that it no longer holds the lock, or the pinned connection is dead — MySQL released the lock together with the session — the pinned connection is dropped and a fresh one re-acquires, instead of falsely reporting the lost lock as still held. A probe that fails on a session still alive is read as still held: a `GET_LOCK` lives exactly as long as its session, so releasing on a stalled answer would admit a second holder into the exclusive section.
- **No TTL.** MySQL advisory locks are connection-lifetime: they do not auto-expire. The `ttl` passed to `CreateLock` is a limit the locker ignores, every state of it admitted, and is **not** honored as an expiry; `bound.UnlimitedDuration()` is the honest spelling — the lock is released by `Release` or when its connection drops (e.g. the process dies). For TTL-based auto-expiry, use the Redis backend in [`integrations/rueidis/v4`](../../../rueidis).
- **`Refresh` verifies ownership.** Because there is nothing to extend, `Refresh` instead confirms the lock is still held on its connection via `SELECT IS_USED_LOCK(?) = CONNECTION_ID()` and returns a "lock is no longer held" error if the lease was lost — matching the lost-lock signal of the in-memory and Redis backends.

See the core [`LOCK.md`](../../../../v4/.documentation/package/LOCK.md) package documentation for the contract and backend comparison.

## Provider

The same module also ships the MySQL dialect [`Provider`](./provider.go) for the core [`bunorm`](../../v4) registry. [`mysql.NewProvider`](./provider.go) takes one [`ProviderConfig`](./provider_config.go); connection details (`Host`, `Port`, `Database`, `User`, `Password`) are supplied at open time through the [`bunorm.ConnectionParameters`](../../v4/connection_parameters.go) the manager registry passes to `Open`, an empty `Port` dialling `3306`. Pool, timeout and retry tuning lives in its [`Pool`](./pool_config.go), [`Timeout`](./timeout_config.go) and [`Retry`](./retry_config.go) fields, and its `PostBuildHook` reaches driver options the typed fields do not expose, through the [`mysql.PostBuildHook`](./post_build_hook.go) signature.

```go
provider := mysql.NewProvider(mysql.ProviderConfig{
    Pool:  mysql.PoolConfig{MaxOpenConnections: bound.Of(50)},
    Retry: mysql.RetryConfig{MaxAttempts: bound.Of(5)},
})
```

Because the provider is given values rather than the configuration keys they came from, arming the framework's credential redaction is the application's call: declare the parameter with the parameter registrar's `RegisterSecretParameter`, or mark one melody registered from the `.env` artifacts with `MarkParameterSecret`. The mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it.

### Defaults

Every limit is a [`bound`](../../../../v4/bound) limit: left out it takes the listed default, `bound.Of(value)` sets it, and `bound.UnlimitedDuration()` / `bound.UnlimitedCount()` lift it where the field admits it. The configuration is resolved once, at `NewProvider`, and a value outside its rule panics there, naming the field (`TimeoutConfig.ConnectTimeout`, for instance), before any database is opened.

| Config          | Field                   | Default | Minimum | Unlimited                                           |
|-----------------|-------------------------|---------|---------|-----------------------------------------------------|
| `PoolConfig`    | `MaxOpenConnections`    | `25`    | `1`     | admitted, no cap                                    |
| `PoolConfig`    | `MaxIdleConnections`    | `5`     | `0`     | admitted, clamped by `database/sql` to the open cap |
| `PoolConfig`    | `ConnectionMaxLifetime` | `5m`    | `1s`    | admitted, never recycled                            |
| `PoolConfig`    | `ConnectionMaxIdleTime` | `1m`    | `1s`    | admitted, never recycled                            |
| `TimeoutConfig` | `ConnectTimeout`        | `10s`   | `1ms`   | refused                                             |
| `TimeoutConfig` | `ReadTimeout`           | `30s`   | `1ms`   | admitted, no deadline                               |
| `TimeoutConfig` | `WriteTimeout`          | `30s`   | `1ms`   | admitted, no deadline                               |
| `RetryConfig`   | `MaxAttempts`           | `3`     | `1`     | refused                                             |
| `RetryConfig`   | `InitialDelay`          | `500ms` | `1ms`   | refused                                             |
| `RetryConfig`   | `MaxDelay`              | `5s`    | `1ms`   | refused                                             |
| `RetryConfig`   | `BackoffMultiplier`     | `2`     | `1`     | a plain float; below `1` or `NaN` refused           |

`MaxIdleConnections: bound.Of(0)` keeps no idle connection, for a serverless or proxy-pooled database. The connect timeout cannot be lifted: it bounds the dial, the handshake, the initial `PingContext` and the post-build hook, and a database that never answers must still fail the open.

A left-out `RetryConfig` is the **default retry**, three attempts; a single attempt is written `RetryConfig{MaxAttempts: bound.Of(1)}`. Only transient failures are retried; a non-transient error fails at once. Every terminal failure is journaled through the logger the open is handed and marked as logged, a single attempt's included.

The lock's own release/verify round trips are bounded separately by [`LockerConfig.ReleaseTimeout`](./lock.go): `5s` by default, at least `1ms`, unlimited refused. A release handed a runtime whose context carries an earlier deadline, still ahead, ends at that one, and the session it could not release on is ended, which frees the lock server-side.

### Recognising a duplicate key, and the connection record

[`mysql.IsDuplicateKey(err)`](./mysql_error.go) answers whether `err`, or any error it wraps, is the driver's `*mysql.MySQLError` with number `1062` (`ER_DUP_ENTRY`). A repository uses it to turn a unique key's refusal into its own domain error instead of a 500:

```go
if _, insertErr := database.NewInsert().Model(row).Exec(ctx); nil != insertErr {
    if true == mysql.IsDuplicateKey(insertErr) {
        return ErrUsernameAlreadyExists
    }

    return insertErr
}
```

It says nothing about which key refused; read the key clause of the driver's message when a table carries more than one.

The failed-open errors carry the `SafeContext` of the `ConnectionParameters` the open was handed: the host, the port, the database and the user, with the password left out, so a diagnostic never prints the credential — but for a cancellation before the attempt, which carries no context.

### Opening under a context, and opening for migrations

- [`Provider.OpenContext`](./provider.go) implements [`bunorm.ContextOpener`](../../v4/provider.go): the retry sleeps watch the caller's context alongside the clock, so a shutdown that cancels it reaches a retry loop in flight instead of sleeping through the whole remaining budget. The registry prefers it and hands the context it was constructed with.
- [`Provider.OpenForMigration`](./provider.go) implements [`bunorm.MigrationProvider`](../../v4/provider.go) and opens the same database with the driver deadlines lifted: `ReadTimeout` and `WriteTimeout` are per-connection settings baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The connect timeout stays armed, the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run.
- [`Provider.OpenForMigrationContext`](./provider.go) implements [`bunorm.MigrationContextOpener`](../../v4/provider.go) — the migration open under the caller's context, the way `OpenContext` is `Open` under it.

### Transport security

The host may be an IPv6 literal, bare (`::1`) or in brackets (`[::1]`), with the port configured separately: a bare literal is bracketed before the port is joined, a bracketed one is kept as written, and a scoped literal such as `fe80::1%eth0` is joined the same way without its zone changing. The default verifying TLS config checks the server certificate against the host without the brackets and without the zone, since no certificate carries either.

The provider negotiates a **verified TLS handshake by default**: it builds a `tls.Config` from the system roots, verifies the server certificate against the configured host, and requires TLS 1.2 or higher. A server that speaks no TLS fails the dial rather than falling back to plaintext, and the driver's `skip-verify` spelling — TLS negotiated but the certificate never checked — is not used, because it is trivially machine-in-the-middled.

Two `ProviderConfig` fields shape it:

- `Insecure: true` disables TLS entirely, leaving a plaintext connection. It is the deliberate opt-out for a database reached over a trusted network or one that speaks no TLS — the same field, spelled the same way, as the pgsql provider.
- `TlsConfig` hands the connector an explicit `*tls.Config` — a pinned server certificate, a client certificate — taking precedence over both the default and `Insecure`.

```go
provider := mysql.NewProvider(mysql.ProviderConfig{
    Insecure: true, // plaintext, for a trusted network or a server without TLS
})
```

### Where bun's own diagnostics go

Bun reports the developer's declaration mistakes — an unknown struct tag option, an unknown `on_update` or `on_delete` rule on a relation, a query carrying arguments and no placeholders — through a package-level logger of its own. Opening a connection through this provider routes that logger into the application's journal, once per process, so those arrive as `warning` records under the message `bun diagnostic` with the line in the context. Without it they are written to standard error as unstructured text, which a deployment whose journal is a json file never sees. The pgsql provider does the same. Bun's logger is one variable for the whole process, so it is set exactly once — but what is set is a forwarder onto a destination every routing REPLACES, so the latest routing wins: a process that builds, closes and rebuilds its application has each lifecycle take the channel back, and a registry's `SetLogger` moves it together with the journal it reports through. A `nil` logger routes
nothing rather than installing a silent destination, and neither does an open refused under an already-cancelled context, so an open handed no journal leaves the channel where the last routing put it — or on standard error, where bun writes when nobody routed at all.

One line does **not** travel that way. When the mysql dialect cannot read the server version it writes

```
can't discover MySQL version: <error>
```

through the **standard library's** default logger, not through bun's, so nothing this provider sets can reach it. Routing it means `log.SetOutput`, which replaces the destination of the standard logger for the whole process — every dependency that logs through it, and your own `log` calls with it. That is the application's decision, not this package's, so if you want it, take it in your composition root:

```go
log.SetOutput(logging.NewStandardErrorLogger(logger, "standard logger").Writer())
```

In practice the line is harmless: bun writes it only after the boot ping has succeeded, the open then returns the database without an error, and no melody record is written for it.
