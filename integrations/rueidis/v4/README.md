# Rueidis integration (v4)

Redis-backed building blocks for Melody v4, built on [`rueidis`](https://github.com/redis/rueidis):

- a [`Provider`](./provider.go) that opens a client from connection parameters under the caller's runtime and answers its owner, a [`Connection`](./connection.go);
- a distributed [`Locker`](./lock.go) implementing the core `lock/contract.Locker`;
- a [`RedisTokenStore`](./token_store.go) implementing the security `RevocableTokenStore`;
- a [`NonceGuard`](./nonce_guard.go) implementing the security `NonceGuard` replay guard;
- a [`RateLimiter`](./rate_limit.go) implementing the HTTP `RateLimiter` contract across instances;
- a [`ServerSentEventBackplane`](./server_sent_event_backplane.go) that fans Server-Sent Events across instances;
- a Redis [`cache`](./cache) backend implementing the core `cache/contract.Backend`.

Import path: `github.com/precision-soft/melody/integrations/rueidis/v4`

## Names

The provider is handed the application namespace, and every component built over a connection derives its default name from it, so applications sharing one Redis differ by their namespace alone. The namespace is the framework's `MELODY_NAMESPACE`, read as `configuration.Kernel().Namespace()`, `melody` when nothing names one; `config.DefaultNamespace` keeps every name of the third major.

| Component | Field | Default |
|---|---|---|
| rate limiter | `RateLimiterConfig.KeyPrefix` | `<namespace>:rate_limit:` |
| server-sent event backplane | `ServerSentEventBackplaneConfig.Channel` | `<namespace>:sse` |
| nonce guard | `NonceGuardConfig.KeyPrefix` | `<namespace>:nonce` |
| token store | `TokenStoreConfig.Prefix` | `<namespace>:token` |
| cache backend | `cache.BackendConfig.Prefix` | `<namespace>:cache:` |
| locker | `LockerConfig.KeyPrefix` | `<namespace>:lock:` |

A name set on a component is used verbatim, the namespace not added to it. A framework lock name already carries the namespace, so the relay's lock is keyed `<namespace>:lock:<namespace>:outbox:relay` on Redis: the name keeps it for the lockers that have no prefix.

## Provider

[`NewProvider(namespace, ProviderConfig)`](./provider.go) builds a [`Provider`](./provider.go) from the application namespace and one [`ProviderConfig`](./provider_config.go) of `Client`, `Retry` and `Logger`, whose zero value is the default; a limit outside its rule panics at `NewProvider`, naming the field, and a namespace outside `config.ValidateNamespace` naming the argument, before any client is built. Every connection `Open` answers carries the namespace. `Open(runtimeInstance, parameters)` takes the caller's runtime and a [`ConnectionParameters`](./connection_parameters.go) literal (`Address`, `User`, `Password`) and answers the [`*Connection`](./connection.go) that owns the client it opened; a comma-separated address list is used as multiple init addresses. The runtime's context bounds the open: a context already done is refused before any client is built, the retry wait ends with it, answering the context's cause with the last dial error under `lastError` (cut at 512 bytes), and the boot ping runs under the earlier of the context's deadline and `ClientConfig.BootPingTimeout`. A nil runtime is refused naming the door. `Ping(runtimeInstance, connection)` runs one round trip under the runtime's context and no deadline of its own.

```go
provider := rueidis.NewProvider(configuration.Kernel().Namespace(), rueidis.ProviderConfig{})

connection, err := provider.Open(resolver.Runtime(), rueidis.ConnectionParameters{Address: address, User: user, Password: password})
if err != nil {
    // connection error
}

client := connection.Client()
```

Configuration, every field left out being the documented default:

- [`ClientConfig`](./client_config.go) — client name, DB selection, TLS, the dial and write deadlines, the boot ping (`BootPingTimeout`, `BootPingDisabled`) and the client-side cache (`ClientSideCacheEnabled`, `ClientSideCacheBytes`). The zero value pings on start and keeps the client-side cache off, so a partial literal is the safe default plus the change; a cache budget set while the cache is not enabled is refused, naming both fields. `BootPingTimeout` bounds the boot ping alone and is not passed into `rueidis.ClientOption`, so ordinary commands are not bounded by it. The network deadline a running application answers to is `ClientConfig.ConnectionWriteTimeout`, which the client also applies as the deadline of a command whose context carries none — and which does not end a READ: the client retries a read-only command on a fresh connection for as long as the context allows, so the budget that ends one is the per-call timeout of the service issuing it (`RateLimiterConfig.CallTimeout`, `TokenStoreConfig.CallTimeout`, `LockerConfig.CallTimeout`, `NonceGuardConfig.CallTimeout`, `ServerSentEventBackplaneConfig.CallTimeout`, `cache.BackendConfig.CallTimeout`).
- [`RetryConfig`](./retry_config.go) — the initial-connection retry (see below).

The dialer and the write deadline are always installed from the resolved values, so the library's own defaults — five seconds for the dial, ten for a write — never govern a melody client.

The provider is handed the connection **values**, not the configuration keys they came from, so it knows no configuration key and names no credential of its own — this package carries no credential-marking door. Arming the framework's redaction is the application's call, through the parameter registrar's `RegisterSecretParameter` for a parameter the application declares, or a `.env` key declared `Secret` in `ApplicationConfig.EnvironmentKeys`, which marks the parameter it feeds; `MarkParameterSecret` takes a parameter name, never a key. The mark propagates to every parameter whose template reads the secret, so a dsn assembled from the credential is redacted with it and `debug:parameters` masks the password in a process that never dials.

Every refusal the provider writes carries the deadlines that governed the attempt — the boot ping timeout and the dial timeout. The password is never part of that record.

### Owning the client

`rueidis.Client.Close` returns nothing, so the raw client cannot join the container's ordered teardown — which closes a value through `CloseWithContext(context.Context) error` or through a closer its registration declared in `RegisterOptions.Closer`, and nothing else; the raw client carries neither. `Open` answers the [`Connection`](./connection.go) that owns the client, whose `CloseWithContext` is the one close: once, the deadline answered as an error. [`NewConnection(client, namespace)`](./connection.go) wraps a client built elsewhere in the same shape, a nil client or a malformed namespace panicking naming the argument; `connection.Namespace()` answers the namespace. Commands through `connection.Client()` are safe while the owner keeps the lifecycle; closing the raw client oneself loses the ordered teardown, the idempotent close and the deadline answer. [`ModuleConfig.Connection`](./module.go) registers the owner's factory as the connection service. Every client-backed service of this package — the locker, the token store, the cache backend — resolves the connection as its dependency, so the teardown closes the connection after them; a run that resolves no connection leaves nothing to close. Every value in this package that could close a client it merely borrows — the cache backend, the rate limiter — deliberately declines to.

### Connection retry

`Open` re-dials a **transient** failure — a Redis that is not accepting connections yet — with capped exponential backoff, so a cold-start race between containers does not hard-fail the boot. An absent [`RetryConfig`](./retry_config.go) is the default retry; a single attempt is written `MaxAttempts: bound.Of(1)`. A non-transient error is never retried and is returned immediately.

```go
provider := rueidis.NewProvider(namespace, rueidis.ProviderConfig{
    Retry: rueidis.RetryConfig{MaxAttempts: bound.Of(10), Backoff: bound.Backoff{Initial: bound.Of(time.Second)}},
})
```

| Field             | Default | Rule                                                                                         |
|-------------------|---------|----------------------------------------------------------------------------------------------|
| `MaxAttempts`     | `3`     | Total attempts, the first included; at least `1`, unlimited refused.                         |
| `Backoff.Initial` | `500ms` | Delay before the second attempt; at least `1ms`, unlimited refused, never above `Max`.       |
| `Backoff.Max`     | `5s`    | Cap on the grown delay; at least `1ms`, unlimited refused.                                   |
| `Backoff.Factor`  | `2.0`   | Growth per attempt, `0` the default; below `1` or `NaN` refused, exactly `1` a constant one. |

Every attempt is reported in the journal `ProviderConfig.Logger` names: a warning per retry, an error when the attempts run out — a single attempt included — and an info line when a later attempt succeeds. The provider is built before the container resolves a logger, so without one the records reach the emergency logger — standard error, outside the application's journal — rather than nothing.

### Defaults

| Config          | Field                    | Default   | Rule                                             |
|-----------------|--------------------------|-----------|--------------------------------------------------|
| `ClientConfig`  | `ClientName`             | empty     |                                                  |
| `ClientConfig`  | `SelectDatabase`         | `0`       |                                                  |
| `ClientConfig`  | `TlsConfig`              | `nil`     | `nil` dials in clear, named off loopback         |
| `ClientConfig`  | `DialTimeout`            | `5s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `ConnectionWriteTimeout` | `5s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `BootPingTimeout`        | `3s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `BootPingDisabled`       | `false`   |                                                  |
| `ClientConfig`  | `ClientSideCacheEnabled` | `false`   |                                                  |
| `ClientConfig`  | `ClientSideCacheBytes`   | `128 MiB` | at least `1`, unlimited refused, needs the cache |

`Open` without `ClientConfig.TlsConfig` over an address that is not `localhost` or a loopback address writes, once per address of the provider, so two applications in one process each write their own line, the warning `rueidis.plaintextAddress` (`PlaintextAddressBootWarningName`) in the runtime's journal, naming the address and never a credential: the password and every value cross the network in clear. A `TlsConfig` is the remedy; the module declares the code, so `MELODY_BOOT_WARNINGS_IGNORED` may name it for a store reached over a network the deployment trusts.

## Distributed lock

[`NewLocker(connection, LockerConfig)`](./lock.go) returns a `lock/contract.Locker`; `CreateLock(name, lockcontract.CreateLockOptions{Ttl: ttl})` returns a `lock/contract.Lock` backed by a Redis key `<namespace>:lock:<name>` with a TTL, and an error for a ttl it refuses. The ttl is a `bound.Duration` resolved through `lockcontract.ResolveLeaseTtl` at `CreateLock` and `lockcontract.ResolveLeaseRefreshTtl` at `Refresh(runtime, lockcontract.RefreshOptions{Ttl: ttl})`: 30s when left out at `CreateLock`, the lock's own ttl when left out at `Refresh`, at least 1ms, and `bound.UnlimitedDuration()` refused, naming `CreateLockOptions.Ttl` or `RefreshOptions.Ttl`, since a key with no expiry would strand the lock forever when its holder died. `Acquire` is a non-blocking try (returns `(false, nil)` when another holder owns the key), `Release` deletes the key only if still owned, and `Refresh` extends the TTL, returning a "lock is no longer held" error if the lease was lost. The locker answers `lockcontract.SharedLeaseLocker`: a lease lives in Redis under its key, reached by every replica of the namespace.

Every door is one Lua round trip on the caller's runtime context, capped at the locker's call timeout; the framework's `RunExclusive` and `LeaderGate` renew under deadlines of their own, a tighter one winning, and release under no deadline of theirs, so the call timeout bounds the release.

An `Acquire` whose reply was lost gives back, detached and best-effort, the lease it may have taken; when that give-back fails as well, the key keeps a token this handle no longer knows, and the same handle is refused like any other until the ttl lapses — the cost of a lost reply is one lost round, bounded by the ttl.

- [`LockerConfig.KeyPrefix`](./lock.go) — opens every lock's key, used verbatim: `<namespace>:lock:` when left out, so applications sharing a store never take each other's locks and no lock lands on a key of another type.
- [`LockerConfig.KeyPrefixDisabled`](./lock.go) — keys a lock by its bare name, as the third major did: the door of a rolling deploy whose replicas still lock bare keys must exclude them; refused beside a `KeyPrefix`, naming both fields.
- [`LockerConfig.CallTimeout`](./lock.go) — bounds one round trip of `Acquire`, `Release` and `Refresh`: 1s when left out, at least 1ms, unlimited refused, a value outside the rule panicking at `NewLocker` naming the field. Unbounded, a store that accepts connections but stops answering holds each call for the client's own connection timeout — five seconds at the provider's default — which is what a readiness handler taking the lock on the request path, or a leader gate campaigning on the caller's context, then waits on every attempt; melody's http kernel attaches no deadline to a request.

## Token store

[`NewTokenStore(connection, TokenStoreConfig)`](./token_store.go) returns a `*RedisTokenStore` implementing the security `RevocableTokenStore`. Every door takes the caller's runtime first, refuses a nil one, and runs each round trip on the runtime's context capped by `CallTimeout`, so a write is cancelled with the request; a context already done is refused before any round trip. A round trip that did not answer returns an error marked with `exception.MarkInfrastructureFailure`, naming `TokenStoreConfig.CallTimeout` when that bound's own deadline passed, and a refused argument returns an unmarked one; no door panics. A stored record that is not JSON is unmarked at `Lookup`, removed by `Delete` and `DeleteByUser` and overwritten by `Put`. Code holding a runtime calls the store through the view bound to it, `security.TokenStoreFromRuntime(runtimeInstance, rueidis.ServiceTokenStore)`, whose doors take no runtime; a provider reads it through `EpochRevocableTokenStoreMustFromResolver`.

A token is stored under `{<prefix>}:token:<hex SHA-256 of the token>`, and the user's index set holds that key, so neither the keyspace, `MONITOR`, the slow log nor a backup holds a credential. The digest is unkeyed, which is sound for a random credential: a token must carry at least 128 bits of entropy, since a guessable one is found from its digest by whoever reads the keyspace. The claims are stored with camelCase keys.

- `Put` — store claims for a token under a `bound.Duration` ttl, read against the security contract's `TokenTtlRule()`: an absent ttl and one under a second are refused naming the ttl, and `bound.UnlimitedDuration()` stores with no expiry, ended only by `Delete`, `DeleteByUser` or `RevokeBefore`. The user identifier must be valid UTF-8, refused at every door that writes or matches it.
- `Lookup` — resolve claims for a token string. An error from it, a failed round trip or an unreadable record, is what the bearer source answers the unavailable token for: `503` with `Retry-After` on a protected route, the request served on a public one.
- `Delete` — revoke a single token.
- `RevokeBefore(runtime, userIdentifier, instant, securitycontract.RevocationOptions{DeviceIdentifier: device})` — publish a revocation boundary, the user-wide one when the options are left out. Every token of that user, or of that one device, issued before the instant stops resolving, including tokens written after the call as long as they were stamped earlier. The boundary only ever moves forward, and the one applied at lookup is the later of the user's and the device's, so a user-wide revocation cannot be undone by a device-scoped one. **This is what ends a user's sessions.** A device identifier, like the user identifier, must be valid UTF-8: the stored claims are json, which rewrites an invalid byte, so `Put` and `RevokeBefore` refuse one naming `Claims.DeviceIdentifier` or `RevocationOptions.DeviceIdentifier`.
- `RevocationEpoch(runtime, userIdentifier, securitycontract.RevocationOptions{DeviceIdentifier: device})` — read the boundary a token would be compared against; the zero instant means nothing has been revoked.

The instants on both sides of that comparison come from application clocks. `TokenStoreConfig.MaximumClockSkew` bounds the window a divergent one opens — it widens the boundary by the stated amount and refuses a stamp further ahead of the verifying node than the same amount — and defaults to zero, so nothing changes unless it is set.

- `DeleteByUser(runtime, userIdentifier)` — reclaim the keys and index entries of a user's tokens; it re-reads each indexed member's owner so a recycled token string belonging to another user is never touched, a token an earlier release stored under its verbatim key with PascalCase claims included. Returns the count removed, what it removed before a failure beside the error. This is **cleanup, not revocation**: the walk is an `SSCAN` cursor, which does not promise to return a member added while it is in progress, so a token issued during the call survives it. Use `RevokeBefore` to make tokens unusable and this to reclaim what it made unusable.
- `PurgeExpired(runtime)` — prune index members whose tokens have expired, answering what it pruned before a failure beside the error.

[`TokenStoreConfig`](./token_store.go), every field left out being the default and a value outside its rule panicking at `NewTokenStore`, naming the field:

- `Prefix` — the key prefix, used verbatim: `<namespace>:token` when left out.
- `CallTimeout` — bounds one round trip of every door: 1s when left out, at least 1ms, unlimited refused. Unbounded, a store that accepts connections but stops answering would hold `Put`, `Delete`, `Lookup` and `RevokeBefore` — each one Lua script, which the client does not retry — for the client's write deadline and hold `DeleteByUser`, `PurgeExpired` and `RevocationEpoch` for good, since the client retries their read-only `SSCAN`, `SCAN` and `HMGET` commands for as long as a context without deadline allows — and melody's http kernel attaches none to a request. Every door keeps a request deadline that is tighter than the timeout, and a round trip the timeout cut answers an infrastructure failure naming this field. The bound is per round trip, so a walk over a large user's index gets one budget per batch.
- `ScanCount` — the batch size of the `SSCAN`/`SCAN` walks: 256 when left out, at least 1, unlimited refused.
- `Clock` — the clock stamping a token's issue instant; nil is the system clock, and the module fills it from the container when the config names none.
- `MaximumClockSkew` — the window the paragraph above describes: 0 when left out, which is no tolerance, at least 0, unlimited refused.
- `RevocationEpochRetention` — how long a revocation boundary outlives its instant: 7 days when left out, at least 1s, and `bound.UnlimitedDuration()` keeps it for ever, as the in-memory store does, at the cost of one hash kept per user that ever revoked.

## Nonce guard

[`NewNonceGuard(connection, NonceGuardConfig)`](./nonce_guard.go) returns a `*NonceGuard` implementing the security `NonceGuard` contract — the shared replay guard for multi-instance HMAC deployments. `Remember(runtime, nonce, ttl)` takes the ttl as a `bound.Duration` read against the security contract's `NonceTtlRule()` — an absent ttl, one under a millisecond and an unlimited one are refused as the error, naming the ttl — and records the nonce with a millisecond expiry in a single atomic round-trip (`SET NX PX` via a Lua script) and reports whether it was already seen, so there is no check-then-set race between instances. Because the recorded nonces live in Redis, a nonce replayed against **any** application instance is detected — something the in-process `security.MemoryNonceGuard` cannot do.

- `NonceGuardConfig.KeyPrefix` — the key prefix, used verbatim; empty is the default `<namespace>:nonce`.
- `NonceGuardConfig.CallTimeout` — bounds one round trip of `Remember`: 1s when left out, at least 1ms, unlimited refused, a value outside the rule panicking at `NewNonceGuard` naming the field. Unbounded, a store that accepts connections but stops answering would hold the record for the client's write deadline — and every authenticator that consults the guard runs on the request path, where melody's http kernel attaches none. A request that already carries a tighter deadline keeps it.

```go
nonceGuard := rueidis.NewNonceGuard(connection, rueidis.NonceGuardConfig{})

tokenSource := security.NewHmacTokenSource(secrets, applications, security.HmacTokenSourceConfig{
    NonceGuard: nonceGuard,
})
```

The same guard fits any `security/contract.NonceGuard` slot, for example the TOTP authenticator's `ReplayGuard`. An error from `Remember` is what the HMAC source answers the unavailable token for: `503` with `Retry-After` on a protected route.

## Rate limiter

[`NewRateLimiter(connection, name, limit, window, RateLimiterConfig)`](./rate_limit.go) is a Redis-backed fixed-window limiter shared by every application instance — the distributed drop-in for the in-process limiters of the core `http/middleware` package. It implements `http/contract.RateLimiter`, so it goes straight into `middleware.RateLimitMiddleware`: `Allow(runtimeInstance, key)` charges one call on the runtime's context capped by the call timeout and answers a `RateLimitDecision`, `Remaining` and `Limit` on every call and `RetryAfter`, the counter's ttl, on a denial, and `Reset(runtimeInstance, key)` drops one counter and answers its failure; `Name()` answers the name it was built with. The counter is incremented atomically in a single Lua round trip, so N instances enforce one shared limit; because the window is fixed rather than sliding, up to twice the limit can pass across a window edge. A counter is keyed `<KeyPrefix><name>:<key>`, under the `<namespace>:rate_limit:` prefix by default, so two limiters over one client never share a counter, a limiter keeps its counters when its limit or window changes, and the limiter shares a Redis instance with the cache, the lock and the token store without colliding. The name is 1 to 64 bytes of `[A-Za-z0-9._-]`, the colon left out so the key splits one way only; a malformed name, a limit below 1 and a window below 1ms panic at construction naming the argument.

```go
limiter := rueidis.NewRateLimiter(connection, "login", 60, time.Minute, rueidis.RateLimiterConfig{})

rateLimit := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{})
```

[`RateLimiterConfig`](./rate_limit.go), every field being the default when left out, and a value outside its rule panicking at `NewRateLimiter`, naming the field:

- `KeyPrefix` — opens every counter, used verbatim; empty is the default `<namespace>:rate_limit:`. A prefix ends with its separator, or `app:x` with the name `y` and `app:` with the name `xy` share a counter.
- `FailureMode` — `FailureModeClosed` when left out, or `FailureModeOpen`; any other value is refused, so a misspelt mode is not read as closed.
- `CallTimeout` — bounds one store round trip on top of the runtime's context: 250ms when left out, at least 1ms, unlimited refused.

**The limiter is fail-closed by default.** When Redis cannot be reached, every limited route denies traffic — the right default for login and one-time-password endpoints, where an outage must not lift the limit, but it also means a Redis outage takes those routes down with it. Pick `FailureModeOpen` deliberately for plain traffic shaping, where a store outage must not become an outage of every limited route. A store failure is returned beside the failure mode's decision, marked with `exception.MarkInfrastructureFailure` under both modes and unrecorded, so the http middleware and `RateLimitGate` record it once and, under the closed mode, answer the denial `503` with the kernel's `Retry-After`, not `429`, and do not call `OnLimitExceeded`; the caller's own cancellation or deadline is not marked. A failed `Reset` is returned to its caller, who records it: a failed reset leaves an account's budget spent after a successful login. Construction panics on a nil connection.

## Server-Sent Event backplane

[`NewServerSentEventBackplane(connection, hub, ServerSentEventBackplaneConfig)`](./server_sent_event_backplane.go) makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a Redis pub/sub channel — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each broadcast is published to a shared channel tagged with a per-instance random origin; a dedicated subscription forwards the events of other instances into the hub via `DeliverLocal` and skips the echo of its own origin, so nothing is delivered twice. The subscription re-subscribes with bounded backoff after a connection drop. The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

```go
hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
backplane := rueidis.NewServerSentEventBackplane(connection, hub, rueidis.ServerSentEventBackplaneConfig{})
defer func() {
    _ = backplane.CloseWithContext(ctx)
}()
```

`NewServerSentEventBackplane` resolves its configuration first and then calls `hub.SetBackplane` itself, so a refused configuration leaves the hub without a backplane and, after construction, `hub.Broadcast(runtimeInstance, topic, event)` replicates automatically. `Publish` runs under the context of the runtime the broadcast was handed: a done context is refused before the event is encoded, with `sse backplane publish refused: the caller's context is done` and the context's cause, a nil runtime refused naming `door` `rueidis.ServerSentEventBackplane.Publish`, and the round trip is capped by `CallTimeout` within that context and ended by the backplane's close. The connection is caller-owned; `CloseWithContext` detaches the backplane and stops the subscription goroutine cleanly, but does not close the client. Delivery is best-effort like Server-Sent Events itself; `hub.BackplaneFailures()` counts broadcasts that could not be published.

[`ServerSentEventBackplaneConfig`](./server_sent_event_backplane.go), every field left out being the default, and a value outside its rule panicking at `NewServerSentEventBackplane`, naming the field:

- `Channel` — the shared pub/sub channel every instance publishes to and subscribes on, used verbatim; empty is the default `<namespace>:sse`, so applications sharing a Redis instance are kept apart by their namespaces.
- `Logger` — a `logging/contract.Logger` for subscription failures and reconnect attempts; nil is the emergency logger, standard error, and a typed nil is refused naming `ServerSentEventBackplaneConfig.Logger`.
- `Reconnect` — the backoff the subscription re-subscribes with after a connection drop, a [`ReconnectConfig`](./reconnect_config.go) whose `Backoff` is a `bound.Backoff`: `Initial` 1s and `Max` 30s when left out, both at least 1ms, unlimited refused; `Factor` 2 when left out, exactly 1 a constant backoff. A factor below 1 or NaN is refused, since it would decay the wait toward zero, and so is an initial backoff above the cap, naming both fields.
- `CallTimeout` — bounds one publish round trip: 1s when left out, at least 1ms, unlimited refused. The caller is typically an http handler broadcasting an event, and its context carries no deadline, so without a bound an unresponsive store holds the request instead of failing it. A publish that runs out of time is counted in `hub.BackplaneFailures()` like any other failed replication — the event reaches the local subscribers but not the other instances — so surface that counter as a metric if cross-instance delivery matters, and raise the timeout for a store whose round trip legitimately exceeds it. The timeout derives from the backplane's own context, so `CloseWithContext` also cancels a publish still in flight.

## Cache backend

Package: [`cache`](./cache). [`cache.NewBackend(connection, BackendConfig)`](./cache/backend.go) wraps the connection's client and exposes one runtime-first door per operation (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`), each round trip bounded by the earlier of the caller's deadline and `CallTimeout`. [`cache.NewBackendService(connection, BackendConfig)`](./cache/backend_service.go) is a container-friendly singleton wrapper implementing the core `cache/contract.Backend`. The `rueidis.Client` is owned by the application, not the backend: `Backend.CloseWithContext` does not close the client, so the same client can be shared with the locker, token store, and server-sent-event backplane without one component tearing it down for the others — close the client once during application shutdown.

`Increment` and `Decrement` write the count and its expiry in one Lua script, as the `cachecontract.CounterOptions` say: `INCRBY`, then `PEXPIRE` when the call creates the key, finds it without an expiry or `RefreshTtl` is set, or `PERSIST` for an unlimited ttl with `RefreshTtl` set, which removes an expiry the key carries; an unlimited ttl without `RefreshTtl` leaves the key's expiry as it is. The cache's redis user needs `EVALSHA` and `EVAL`, and, since a script's commands run under the caller's permissions, the commands the script runs: `INCRBY`, `PTTL`, `PEXPIRE`, `PERSIST` and `GET`.

`Backend.CloseWithContext` does end the backend: every later operation answers `cache backend is closed` rather than serving over a client whose owner already tore this backend down. A refusal of a malformed key names it by its first 128 bytes, the length beside it. `ClearByPrefix` refuses the empty prefix rather than reading it as the whole namespace: a prefix assembled at run time that comes out empty would otherwise wipe everything, which is the one outcome a prefixed delete exists to prevent — `Clear` is the door that means the whole namespace.

[`BackendConfig`](./cache/backend.go), every field left out being the default, and a value outside its rule answered as the error of `NewBackend` and `NewBackendService`, naming the field:

- `Prefix` — the whole of the backend's isolation, used verbatim; empty is the default `<namespace>:cache:`, so applications and environments sharing a store are kept apart by their namespaces.
- `ScanCount` — the `COUNT` hint of the `SCAN` walk `Clear` and `ClearByPrefix` run: 1000 when left out, at least 1, unlimited refused.
- `DeleteBatch` — how many keys one `DEL` carries: 500 when left out, at least 1, unlimited refused.
- `MaxKeyLength` — the key bound the refusals are measured against, on the key the caller hands in, before the prefix: 1024 when left out, at least 1, unlimited refused.
- `CallTimeout` — bounds every round trip on top of the runtime's context, the earlier deadline winning, each `SCAN` and `DEL` round trip of a `Clear` or a `ClearByPrefix` on its own while the runtime's context bounds the walk, so the size of a namespace is not capped by it: 1s when left out, at least 1ms, unlimited refused. Unbounded, a request-path read against a store that accepts connections but stops answering would be retried for as long as the context allows and hang the handler. A round trip the call timeout cut answers `cache round trip was cut by BackendConfig.CallTimeout`, naming the field, the timeout and the door, with `cache.ErrCallTimeout` in its chain; one the caller's own deadline or cancellation cut answers the context's error without it, wrapped on `Increment` and `Decrement` under `cache counter operation failed`, so `errors.Is` holds where the identity and the message do not. Both still match `context.DeadlineExceeded`:

```go
if true == errors.Is(err, cache.ErrCallTimeout) {
    /* the store was slow, not the caller out of time */
}
```

### BackendFromRuntime

Helper: [`cache.BackendFromRuntime`](./cache/backend_service.go)

Returns the registered service's `*Backend`; every operation takes the caller's runtime first, so the request's deadline travels with each call. Despite carrying no `Must` in its name, it panics when the service is absent or mistyped — the signature has no error slot to answer through, so treat it as the Must door it is:

```go
package main

func main() {
    backend := rueidiscache.BackendFromRuntime(runtimeInstance, cache.ServiceCacheBackend)
    backend.Get(runtimeInstance, "my-key")
}
```

## Plug-and-play registration

The modules are the registration door. [`rueidis.NewModule`](./module.go) registers the connection service its `Connection` factory builds and, each behind a pointer to its configuration, the locker and the revocable token store over it; [`rueidiscache.NewModule`](./cache/module.go) registers the cache backend over the same connection service. A provider resolves each through the matching `*MustFromResolver` helper, and a handler holding a runtime reads the client of the connection with `ClientMustFromRuntime(runtimeInstance)`:

```go
app.RegisterModule(rueidis.NewModule(rueidis.ModuleConfig{
    Connection: func(resolver containercontract.Resolver) (*rueidis.Connection, error) {
        return provider.Open(resolver.Runtime(), parameters)
    },
    ConnectionBuiltAtBoot: true,
    ConnectionBootFailure: container.BootFailureFailsBoot,
    Locker:                &rueidis.LockerConfig{},
    TokenStore:            &rueidis.TokenStoreConfig{},
}))
app.RegisterModule(rueidiscache.NewModule(rueidiscache.ModuleConfig{Backend: rueidiscache.BackendConfig{}}))
```

| Field                   | Registers                                                   | Left out                                                  |
|-------------------------|-------------------------------------------------------------|-----------------------------------------------------------|
| `Connection`            | `rueidis.ServiceConnection`, `"service.rueidis.connection"` | nothing                                                   |
| `ConnectionBuiltAtBoot` | the connection built inside `Boot`, under the boot runtime  | built at its first resolution                             |
| `ConnectionBootFailure` | the registrar's boot failure mode of that build             | `BootFailureWarns`                                        |
| `Locker`                | core `lock.ServiceLocker`                                   | no locker; `&LockerConfig{}` is the default locker        |
| `TokenStore`            | `rueidis.ServiceTokenStore`, `"service.rueidis.token_store"` | no store; `&TokenStoreConfig{}` is the default store      |

The module copies the configurations it is handed at `NewModule`, so a later edit of the caller's value is not observed, and resolves each at `RegisterServices`, so a value outside its rule fails the boot rather than the first resolve. A role with no `Connection`, `ConnectionBuiltAtBoot` with no `Connection`, and a `ConnectionBootFailure` with no connection built at boot are refused at boot naming the field. In the warning mode a cli process builds the connection at its first use; `BootFailureFailsBoot` connects in every process, a command included, and fails its boot when the store does not answer. The cache backend declares the connection as the service it is closed before, so an application that registers the cache module and no connection is refused at boot.
