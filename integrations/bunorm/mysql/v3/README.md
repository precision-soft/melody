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

## Semantics

- **Try-acquire only.** `Acquire` issues `SELECT GET_LOCK(?, 0)` — a non-blocking attempt that returns immediately, consistent with the in-memory and Redis backends. A failed acquisition returns `(false, nil)`, not an error.
- **Session-pinned.** Each held lock owns a dedicated `*sql.Conn` that is pinned for the lifetime of the lock. MySQL advisory locks are scoped to the connection that took them, so `Release` (`DO RELEASE_LOCK(?)`) and the ownership check run on that same connection, which is then closed and returned to the pool.
- **Reentrant within a `Lock`.** Calling `Acquire` again on a lock that is already held returns `(true, nil)` without re-querying.
- **No TTL.** MySQL advisory locks are connection-lifetime: they do not auto-expire. The `ttl` passed to `CreateLock` is accepted only for interface compatibility and is **not** honored as an expiry — the lock is released by `Release` or when its connection drops (e.g. the process dies). For TTL-based auto-expiry, use the Redis backend in [`integrations/rueidis/v3`](../../../rueidis).
- **`Refresh` verifies ownership.** Because there is nothing to extend, `Refresh` instead confirms the lock is still held on its connection via `SELECT IS_USED_LOCK(?) = CONNECTION_ID()` and returns a "lock is no longer held" error if the lease was lost — matching the lost-lock signal of the in-memory and Redis backends.

See the core [`LOCK.md`](https://github.com/precision-soft/melody) package documentation for the contract and backend comparison.
