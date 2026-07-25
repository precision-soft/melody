# LOCK

The [`lock`](../../lock) package provides a distributed/named lock abstraction: a `Locker` creates named `Lock` values that can be acquired, released, and refreshed. The core package ships a dependency-free in-memory implementation; durable backends live in integrations.

## Scope

Locking is opt-in. The core defines the contract and an in-memory `Locker` (single-process, useful for tests and single-instance deployments). For cross-process locking, use an integration-backed `Locker` — Redis via [`rueidis`](../../../integrations/rueidis), MySQL `GET_LOCK` via [`bunorm/mysql`](../../../integrations/bunorm/mysql), or PostgreSQL session advisory locks via [`bunorm/pgsql`](../../../integrations/bunorm/pgsql) — which implement the same contract.

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
- Provide the patterns built on the contract, so consumers do not hand-roll them:
    - [`RunExclusive`](../../lock/run_exclusive.go) — run-once-per-tick around a callback
    - [`ExclusiveCommand`](../../lock/exclusive_command.go) — the same, as a CLI command decorator
    - [`LeaderGate`](../../lock/leader_gate.go) — become leader, renew, release on shutdown
    - [`NewLazyLocker`](../../lock/lazy_locker.go) — a `Locker` that resolves the registered one on first use
- Provide container resolver helpers:
    - [`ServiceLocker`](../../lock/service_resolver.go)
    - [`LockerMustFromContainer`](../../lock/service_resolver.go)
    - [`LockerMustFromResolver`](../../lock/service_resolver.go)

## Semantics

- `Acquire` is a single, non-blocking attempt: it returns `(true, nil)` when the lock is taken and `(false, nil)` when it is already held by someone else.
- A `Lock` value owns its acquisition via a per-instance token. `Release` and `Refresh` only affect the lock when this instance still holds it. Re-acquiring with the same `Lock` instance is reentrant.
- `Release` is best-effort and idempotent: releasing a lock this instance no longer holds is a no-op and reports no error (the Redis convention). `Refresh` is the authoritative liveness check — it returns an error when the lease has been lost. Use `Refresh`, not `Release`, to detect a lost lock.
- `ttl` is the lease duration. [`InMemoryLocker`](../../lock/in_memory.go) expires the holder after `ttl` (a `ttl` of `0` passed to `CreateLock` never expires); `Refresh` requires a **positive** `ttl` and returns an error otherwise, so a refresh cannot accidentally turn a leased lock into a permanent one. The in-memory locker opportunistically purges expired holders during `Acquire`; call `PurgeExpired()` (for example from a periodic task) to reclaim memory for locks that expire without an explicit `Release`. The Redis backend sets the key TTL; the MySQL and PostgreSQL backends have no TTL (their locks are held until release or connection close), so their `Refresh` has nothing to extend — it instead verifies the lock is still held on its connection and returns an error if it has been lost, matching the lost-lock signal of the other backends.

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

PostgreSQL-backed (`integrations/bunorm/pgsql`), the Postgres counterpart of the MySQL locker, built on session advisory locks (`pg_try_advisory_lock` / `pg_advisory_unlock`); lock names are hashed (FNV-1a, 64-bit) onto PostgreSQL's integer advisory-lock keys:

```go
locker := pgsql.NewLocker(database)
```

### Run a block exclusively

[`RunExclusive`](../../lock/run_exclusive.go) acquires the named lock, runs the callback while holding it, and always releases afterwards, so the `ttl` acts only as crash-safety and never as the run cadence. It returns `(false, nil)` **without running the callback** when another holder owns the lock — which is what makes N cron-launched instances run the same work exactly once per tick.

```go
ran, runErr := lock.RunExclusive(
    runtimeInstance,
    locker,
    "billing:settle",
    30*time.Second,
    func(childRuntime runtimecontract.Runtime) error {
        return settleInvoices(childRuntime)
    },
)
```

While the callback runs, the lease is refreshed on a background goroutine at half the `ttl`. A failed refresh **cancels the child runtime** handed to the callback — the lease may now belong to another instance, so leader-only work must stop rather than keep going alongside it — and `RunExclusive` then returns `(true, <exclusive run lost the lock lease while running>)` wrapping the refresh failure and carrying the callback's own error in its context.

An `Acquire` that errors fails **closed**: `(false, err)`, because an unreachable store must not double-run the work. A shutdown is not an error: when the runtime context is already cancelled, an `Acquire` that fails with that cancellation returns `(false, nil)`, so a graceful stop does not read as a failed run. The release always runs on a detached, 5-second-bounded context, so a `SIGTERM` between a cron tick and its release cannot leave the lock held until the `ttl` lapses.

A non-positive `ttl` selects session-lock behaviour: there is no lease to extend, so the renewal becomes a liveness probe every 15 seconds.

### Decorate a CLI command

[`NewExclusiveCommand`](../../lock/exclusive_command.go) wraps any `cli/contract.Command` in `RunExclusive` — the per-tick dedup for cron-launched commands on a multi-instance deployment. The instances that do not get the lock **skip quietly with a zero exit code** (logging at info level through the runtime logger), so cron stays green everywhere.

```go
command := lock.NewExclusiveCommand(
    command.NewBillingSettleCommand(),
    locker,
    30*time.Second,
)
```

The lock name defaults to `"melody:command:"` plus the wrapped command's name. [`NewExclusiveCommandWithName`](../../lock/exclusive_command.go) takes it explicitly, for when two differently-named commands must share one lock or one command needs distinct locks per deployment. `Name`, `Description` and `Flags` delegate to the wrapped command, so the decorator is transparent to the CLI.

### Elect a leader

[`NewLeaderGate`](../../lock/leader_gate.go) is the become-leader, renew-periodically, release-on-shutdown pattern for a long-running worker. `Run` blocks until the runtime context is cancelled and always returns `nil` on a clean shutdown, so start it with `go gate.Run(runtimeInstance)` and gate the work on `IsLeader()`, or hook `OnElected`/`OnLost`.

```go
gate := lock.NewLeaderGateWithOptions(
    locker,
    "importer:leader",
    30*time.Second,
    lock.LeaderGateOptions{
        OnElected: func(termRuntime runtimecontract.Runtime) {
            runImportLoop(termRuntime)
        },
        OnLost: func(termRuntime runtimecontract.Runtime, cause error) {
            logLostLeadership(cause)
        },
        OnCampaignError: func(termRuntime runtimecontract.Runtime, cause error) {
            logStoreOutage(cause)
        },
    },
)

go gate.Run(runtimeInstance)
```

[`LeaderGateOptions`](../../lock/leader_gate.go) — every zero value resolves to a default derived from the `ttl`:

| Field             | Meaning                                                                                                                                | Default                                                                        |
|-------------------|----------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------|
| `RetryInterval`   | pause between failed campaigns while another instance leads                                                                            | half the `ttl`, floored at 1s                                                  |
| `RefreshInterval` | lease-renewal cadence while leading                                                                                                    | half the `ttl`; 15s for a non-positive `ttl`. An override above `ttl/2` is clamped down to it, because a cadence slower than half the lease lets the lease lapse before the first refresh and two instances would both report leadership |
| `OnElected`       | runs on the `Run` goroutine right after election. Its runtime carries a context cancelled when the lease is lost, so leader-only work stops instead of running alongside the new holder. While it blocks, the gate cannot campaign again | none                                                                           |
| `OnLost`          | runs right after leadership is lost to a failed renewal; `cause` is the renewal error. Does **not** run on a clean shutdown            | none                                                                           |
| `OnCampaignError` | runs for every campaign that could not even ask the store who leads                                                                    | none                                                                           |

**The lease renewal is started before `OnElected` is invoked, and keeps running for the whole time the hook runs** — so a hook that legitimately outlives the `ttl` does not lose the lease under itself. Were it the other way round, nothing would renew while the hook worked: the lease would lapse, a second instance could acquire it, and this one would still report leadership and never demote, because demotion only follows a *failed* renewal.

`Run` never aborts on an `Acquire` error (a store outage): it backs off by doubling, capped at 1 minute but never faster than `RetryInterval`, and resumes campaigning. Because such errors never abort it, they are also never returned — **wire `OnCampaignError`**, or a permanent misconfiguration (for example a Redis locker built with a non-positive `ttl`, whose `Acquire` fails closed on every call) is indistinguishable from a deployment that simply has no work to lead.

Each campaign creates a **fresh** `Lock`: every `CreateLock` mints a new fencing token, and reusing a lock after losing it would alias tokens with whoever took it over.

### Resolve the locker lazily

A CLI command or an HTTP middleware assembled during boot needs a `Locker` before the container is safe to resolve from. [`NewLazyLocker(resolver)`](../../lock/lazy_locker.go) returns a `Locker` that resolves `service.lock.locker` on first `CreateLock` and reuses a successful resolution thereafter.

```go
locker := lock.NewLazyLocker(resolver)

exclusiveCommand := lock.NewExclusiveCommand(inner, locker, 30*time.Second)
```

A failed resolution never panics: `CreateLock` hands back a lock whose every method reports the resolution error, so `LeaderGate` and `RunExclusive` see it as the acquire error they already handle (and fail closed), and the resolution is retried on the next `CreateLock`.

## Footguns & caveats

- Locking is opt-in and userland-wired; the framework registers no default `Locker`.
- [`InMemoryLocker`](../../lock/in_memory.go) is single-process only — it does not coordinate across instances. Use a Redis or MySQL backend for horizontal scaling.
- MySQL `GET_LOCK` is per-session: the backend pins a dedicated connection for the lifetime of a held lock and releases it on `Release`. It has no lease expiry, so a crashed process releases the lock only when its connection closes.
- A reentrant `Acquire` on a MySQL lock re-verifies that its pinned connection still holds the lock before returning `(true, nil)`; if that connection was dropped (so MySQL already released the lock), it transparently re-acquires on a fresh connection instead of falsely reporting the lost lock as still held.
- PostgreSQL session advisory locks mirror the MySQL semantics: the backend pins a dedicated connection per held lock (released on `Release` or connection close, so a crashed process releases the lock when its connection closes) and a reentrant `Acquire` re-verifies the pinned connection still holds the lock before returning `(true, nil)`. Release always runs on a fresh context, so a cancelled request context cannot leave the advisory lock held on a connection returned to the pool. Names are FNV-1a-hashed to the 64-bit advisory key, so two distinct names can in principle collide onto the same lock.
- `Acquire` does not block or retry; implement waiting in userland if needed — or use [`RunExclusive`](../../lock/run_exclusive.go) / [`LeaderGate`](../../lock/leader_gate.go), which campaign on a cadence rather than blocking.
- `RunExclusive` and `LeaderGate` both **fail closed** on an acquire error: the work does not run. A store outage therefore stops the exclusive work rather than risking two instances doing it at once.
- `LeaderGate.Run` swallows acquire errors by design and returns `nil` on a clean shutdown, so its return value carries no diagnostic. Without `OnCampaignError` a permanently broken locker looks exactly like a healthy deployment with no work to lead.
- `ExclusiveCommand` exits **zero** when another instance holds the lock. That is what keeps a cron fleet green, but it also means "did not run" is not distinguishable from "ran successfully" by exit code alone; read the info-level skip log instead.
- A non-positive `ttl` handed to `RunExclusive` or `LeaderGate` selects session-lock behaviour (probe instead of renew). Passing one to a *lease* backend such as Redis is a misconfiguration, not a mode switch: the Redis locker's `Acquire` fails closed on every call.

## Userland API

### Contracts (`lock/contract`)

- [`Lock`](../../lock/contract/lock.go)
- [`Locker`](../../lock/contract/lock.go)

### Types and constructors (`lock`)

- [`InMemoryLocker`](../../lock/in_memory.go)
- [`NewInMemoryLocker(clockInstance clockcontract.Clock) *InMemoryLocker`](../../lock/in_memory.go)
    - `PurgeExpired() int` — reclaims memory for locks that expired without an explicit `Release`, returning the count purged

### Exclusive execution (`lock`)

- [`RunExclusive(runtimeInstance runtimecontract.Runtime, locker lockcontract.Locker, name string, ttl time.Duration, callback func(runtimecontract.Runtime) error) (bool, error)`](../../lock/run_exclusive.go)
- [`ExclusiveCommand`](../../lock/exclusive_command.go) — implements `cli/contract.Command`
    - [`NewExclusiveCommand(command clicontract.Command, locker lockcontract.Locker, ttl time.Duration) *ExclusiveCommand`](../../lock/exclusive_command.go)
    - [`NewExclusiveCommandWithName(command clicontract.Command, locker lockcontract.Locker, lockName string, ttl time.Duration) *ExclusiveCommand`](../../lock/exclusive_command.go)

### Leader election (`lock`)

- [`LeaderGate`](../../lock/leader_gate.go)
    - [`NewLeaderGate(locker lockcontract.Locker, name string, ttl time.Duration) *LeaderGate`](../../lock/leader_gate.go)
    - [`NewLeaderGateWithOptions(locker lockcontract.Locker, name string, ttl time.Duration, options LeaderGateOptions) *LeaderGate`](../../lock/leader_gate.go)
    - `Run(runtimeInstance runtimecontract.Runtime) error`
    - `IsLeader() bool`
- [`LeaderGateOptions`](../../lock/leader_gate.go) — `RetryInterval`, `RefreshInterval`, `OnElected`, `OnLost`, `OnCampaignError`

### Container helpers (`lock`)

- [`const ServiceLocker`](../../lock/service_resolver.go) (`"service.lock.locker"`)
- [`LockerMustFromContainer(containercontract.Container) lockcontract.Locker`](../../lock/service_resolver.go)
- [`LockerMustFromResolver(containercontract.Resolver) lockcontract.Locker`](../../lock/service_resolver.go)
- [`NewLazyLocker(resolver containercontract.Resolver) lockcontract.Locker`](../../lock/lazy_locker.go)
