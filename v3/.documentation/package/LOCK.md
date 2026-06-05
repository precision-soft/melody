# LOCK

The [`lock`](../../lock) package provides a distributed/named lock abstraction: a `Locker` creates named `Lock` values that can be acquired, released, and refreshed. The core package ships a dependency-free in-memory implementation; durable backends live in integrations.

## Scope

Locking is opt-in. The core defines the contract and an in-memory `Locker` (single-process, useful for tests and single-instance deployments). For cross-process locking, use an integration-backed `Locker` — Redis via [`rueidis`](../../../integrations/rueidis) or MySQL `GET_LOCK` via [`bunorm/mysql`](../../../integrations/bunorm/mysql) — which implement the same contract.

## Subpackages

- [`lock/contract`](../../lock/contract)  
  Public contracts for `Lock` and `Locker`.

## Responsibilities

- Define the abstraction:
    - [`Lock`](../../lock/contract/lock.go) — `Acquire` (non-blocking try), `Release`, `Refresh(ttl)`
    - [`Locker`](../../lock/contract/lock.go) — `CreateLock(name, ttl)`
- Provide an in-memory implementation:
    - [`InMemoryLocker`](../../lock/in_memory.go)
    - [`NewInMemoryLocker`](../../lock/in_memory.go)
- Provide container resolver helpers:
    - [`ServiceLocker`](../../lock/service_resolver.go)
    - [`LockerMustFromContainer`](../../lock/service_resolver.go)
    - [`LockerMustFromResolver`](../../lock/service_resolver.go)

## Semantics

- `Acquire` is a single, non-blocking attempt: it returns `(true, nil)` when the lock is taken and `(false, nil)` when it is already held by someone else.
- A `Lock` value owns its acquisition via a per-instance token. `Release` and `Refresh` only affect the lock when this instance still holds it. Re-acquiring with the same `Lock` instance is reentrant.
- `Release` is best-effort and idempotent: releasing a lock this instance no longer holds is a no-op and reports no error (the Redis convention). `Refresh` is the authoritative liveness check — it returns an error when the lease has been lost. Use `Refresh`, not `Release`, to detect a lost lock.
- `ttl` is the lease duration. [`InMemoryLocker`](../../lock/in_memory.go) expires the holder after `ttl` (a `ttl` of `0` passed to `CreateLock` never expires); `Refresh` requires a **positive** `ttl` and returns an error otherwise, so a refresh cannot accidentally turn a leased lock into a permanent one. The in-memory locker opportunistically purges expired holders during `Acquire`; call `PurgeExpired()` (for example from a periodic task) to reclaim memory for locks that expire without an explicit `Release`. The Redis backend sets the key TTL; the MySQL backend has no TTL (user locks are held until release or connection close), so its `Refresh` has nothing to extend — it instead verifies the lock is still held on its connection and returns an error if it has been lost, matching the lost-lock signal of the other backends.

## Usage

```go
locker := lock.NewInMemoryLocker(clock.NewSystemClock())

pickingLock := locker.CreateLock("picking:order:42", 30*time.Second)

acquired, acquireErr := pickingLock.Acquire(runtimeInstance)
if nil != acquireErr {
	return acquireErr
}

if false == acquired {
	return nil
}

defer pickingLock.Release(runtimeInstance)
```

Redis-backed (`integrations/rueidis`):

```go
locker := rueidis.NewLocker(client)
```

MySQL-backed (`integrations/bunorm/mysql`):

```go
locker := mysql.NewLocker(database)
```

## Footguns & caveats

- Locking is opt-in and userland-wired; the framework registers no default `Locker`.
- [`InMemoryLocker`](../../lock/in_memory.go) is single-process only — it does not coordinate across instances. Use a Redis or MySQL backend for horizontal scaling.
- MySQL `GET_LOCK` is per-session: the backend pins a dedicated connection for the lifetime of a held lock and releases it on `Release`. It has no lease expiry, so a crashed process releases the lock only when its connection closes.
- `Acquire` does not block or retry; implement waiting in userland if needed.

## Userland API

### Contracts (`lock/contract`)

- [`Lock`](../../lock/contract/lock.go)
- [`Locker`](../../lock/contract/lock.go)

### Types and constructors (`lock`)

- [`InMemoryLocker`](../../lock/in_memory.go)
- [`NewInMemoryLocker(clockInstance clockcontract.Clock) *InMemoryLocker`](../../lock/in_memory.go)

### Container helpers (`lock`)

- [`const ServiceLocker`](../../lock/service_resolver.go)
- [`LockerMustFromContainer(containercontract.Container) lockcontract.Locker`](../../lock/service_resolver.go)
- [`LockerMustFromResolver(containercontract.Resolver) lockcontract.Locker`](../../lock/service_resolver.go)
