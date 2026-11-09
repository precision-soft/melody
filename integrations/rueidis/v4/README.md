# Rueidis integration (v4)

Redis-backed building blocks for Melody v4, built on [`rueidis`](https://github.com/redis/rueidis):

- a [`Provider`](./provider.go) that opens a `rueidis.Client` from connection parameters;
- a distributed [`Locker`](./lock.go) implementing the core `lock/contract.Locker`;
- a [`RedisTokenStore`](./token_store.go) implementing the security `RevocableTokenStore`;
- a [`NonceGuard`](./nonce_guard.go) implementing the security `NonceGuard` replay guard;
- a [`RateLimiter`](./rate_limit.go) implementing the HTTP `RateLimiter` / `RuntimeRateLimiter` contracts across instances;
- a [`ServerSentEventBackplane`](./server_sent_event_backplane.go) that fans Server-Sent Events across instances;
- a Redis [`cache`](./cache) backend implementing the core `cache/contract.Backend`.

Import path: `github.com/precision-soft/melody/integrations/rueidis/v4`

## Provider

[`NewProvider(ProviderConfig)`](./provider.go) builds a [`Provider`](./provider.go) from one [`ProviderConfig`](./provider_config.go) of `Client`, `Timeout`, `Retry` and `Logger`, whose zero value is the default; a limit outside its rule panics at `NewProvider`, naming the field, before any client is built. `Open` takes a [`ConnectionParameters`](./connection_parameters.go) literal (`Address`, `User`, `Password`) and returns a `rueidis.Client`; a comma-separated address list is used as multiple init addresses.

```go
provider := rueidis.NewProvider(rueidis.ProviderConfig{})

client, err := provider.Open(rueidis.ConnectionParameters{Address: address, User: user, Password: password})
if err != nil {
    // connection error
}
```

Configuration, every field left out being the documented default:

- [`ClientConfig`](./client_config.go) — client name, DB selection, TLS, the dial and write deadlines, `BootPingDisabled` and the client-side cache (`ClientSideCacheEnabled`, `ClientSideCacheBytes`). The zero value pings on start and keeps the client-side cache off, so a partial literal is the safe default plus the change; a cache budget set while the cache is not enabled is refused, naming both fields.
- [`TimeoutConfig`](./timeout_config.go) — **boot only**. `ConnectTimeout` and `CommandTimeout` bound the provider's own ping round trips; neither is passed into `rueidis.ClientOption`, so ordinary commands are not bounded by them. The network deadline a running application answers to is `ClientConfig.ConnWriteTimeout`, which the client also applies as the deadline of a command whose context carries none — and which does not end a READ: the client retries a read-only command on a fresh connection for as long as the context allows, so the budget that ends one is the per-call timeout of the service issuing it (`RateLimiterConfig.CallTimeout`, `TokenStoreConfig.CallTimeout`, `LockerConfig.CallTimeout`, `NonceGuardConfig.CallTimeout`, `ServerSentEventBackplaneConfig.CallTimeout`, `cache.BackendConfig.CommandTimeout`).
- [`RetryConfig`](./retry_config.go) — the initial-connection retry (see below).

The dialer and the write deadline are always installed from the resolved values, so the library's own defaults — five seconds for the dial, ten for a write — never govern a melody client.

The provider is handed the connection **values**, not the configuration keys they came from, so it knows no configuration key and names no credential of its own — this package carries no credential-marking door. Arming the framework's redaction is the application's call, through the parameter registrar's `RegisterSecretParameter` for a parameter the application declares, or `MarkParameterSecret` for one melody registered from the `.env` artifacts. The mark propagates to every parameter whose template reads the secret, so a dsn assembled from the credential is redacted with it and `debug:parameters` masks the password in a process that never dials.

Every refusal the provider writes carries the deadlines that governed the attempt — the connect timeout and the dial timeout. The password is never part of that record.

### Owning the client

`rueidis.Client.Close` returns nothing, so the raw client cannot join the container's ordered teardown — which closes a value through `CloseWithContext(context.Context) error` or through a closer its registration declared in `RegisterOptions.Closer`, and nothing else; the raw client carries neither. [`NewConnection`](./connection.go) wraps it in that shape, and [`RegisterConnectionService`](./service_resolver.go) (or [`ModuleConfig.Connection`](./module.go)) registers it as the service that owns the client. Every client-backed provider in this package — the client itself, the locker, the token store, the cache backend — resolves the registered connection as its dependency, so whichever run resolves one of them orders the connection's close after it; a run that resolves none leaves the connection the composition root's to close, since the container closes only what was resolved at
least once. Every value in this package that could close a client it merely borrows — the cache backend, the rate limiter — deliberately declines to.

### Connection retry

`Open` re-dials a **transient** failure — a Redis that is not accepting connections yet — with capped exponential backoff, so a cold-start race between containers does not hard-fail the boot. An absent [`RetryConfig`](./retry_config.go) is the default retry; a single attempt is written `MaxAttempts: bound.Of(1)`. A non-transient error is never retried and is returned immediately.

```go
provider := rueidis.NewProvider(rueidis.ProviderConfig{
    Retry: rueidis.RetryConfig{MaxAttempts: bound.Of(10), InitialDelay: bound.Of(time.Second)},
})
```

| Field               | Default | Rule                                                                                         |
|---------------------|---------|----------------------------------------------------------------------------------------------|
| `MaxAttempts`       | `3`     | Total attempts, the first included; at least `1`, unlimited refused.                         |
| `InitialDelay`      | `500ms` | Delay before the second attempt; at least `1ms`, unlimited refused.                          |
| `MaxDelay`          | `5s`    | Cap on the grown delay; at least `1ms`, unlimited refused.                                   |
| `BackoffMultiplier` | `2.0`   | Growth per attempt, `0` the default; below `1` or `NaN` refused, exactly `1` a constant one. |

Every attempt is reported in the journal `ProviderConfig.Logger` names: a warning per retry, an error when the attempts run out — a single attempt included — and an info line when a later attempt succeeds. The provider is built before the container resolves a logger, so without one the records reach the emergency logger — standard error, outside the application's journal — rather than nothing.

### Defaults

| Config          | Field                    | Default   | Rule                                             |
|-----------------|--------------------------|-----------|--------------------------------------------------|
| `TimeoutConfig` | `ConnectTimeout`         | `3s`      | at least `1ms`, unlimited refused                |
| `TimeoutConfig` | `CommandTimeout`         | `3s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `ClientName`             | empty     |                                                  |
| `ClientConfig`  | `SelectDb`               | `0`       |                                                  |
| `ClientConfig`  | `TlsConfig`              | `nil`     |                                                  |
| `ClientConfig`  | `DialTimeout`            | `5s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `ConnWriteTimeout`       | `5s`      | at least `1ms`, unlimited refused                |
| `ClientConfig`  | `BootPingDisabled`       | `false`   |                                                  |
| `ClientConfig`  | `ClientSideCacheEnabled` | `false`   |                                                  |
| `ClientConfig`  | `ClientSideCacheBytes`   | `128 MiB` | at least `1`, unlimited refused, needs the cache |

## Distributed lock

[`NewLocker(client, LockerConfig)`](./lock.go) returns a `lock/contract.Locker`; `CreateLock(name, ttl)` returns a `lock/contract.Lock` backed by a Redis key with a TTL. The ttl is a `bound.Duration` resolved through `lockcontract.ResolveLeaseTtl` at `Acquire` and `lockcontract.ResolveLeaseRefreshTtl` at `Refresh`: 30s when left out, at least 1ms, and `bound.UnlimitedDuration()` refused, naming `Locker.CreateLock.ttl` or `Lock.Refresh.ttl`, since a key with no expiry would strand the lock forever when its holder died. `Acquire` is a non-blocking try (returns `(false, nil)` when another holder owns the key), `Release` deletes the key only if still owned, and `Refresh` extends the TTL, returning a "lock is no longer held" error if the lease was lost.

Every door is one Lua round trip on the caller's runtime context, capped at the locker's call timeout; the framework's `RunExclusive` and `LeaderGate` renew and release under deadlines of their own, and a tighter one always wins.

An `Acquire` whose reply was lost gives back, detached and best-effort, the lease it may have taken; when that give-back fails as well, the key keeps a token this handle no longer knows, and the same handle is refused like any other until the ttl lapses — the cost of a lost reply is one lost round, bounded by the ttl.

- [`LockerConfig.CallTimeout`](./lock.go) — bounds one round trip of `Acquire`, `Release` and `Refresh`: 1s when left out, at least 1ms, unlimited refused, a value outside the rule panicking at `NewLocker` naming the field. Unbounded, a store that accepts connections but stops answering holds each call for the client's own connection timeout — five seconds at the provider's default — which is what a readiness handler taking the lock on the request path, or a leader gate campaigning on the caller's context, then waits on every attempt; melody's http kernel attaches no deadline to a request.

## Token store

[`NewTokenStore(client, TokenStoreConfig)`](./token_store.go) returns a `*RedisTokenStore` implementing the security `RevocableTokenStore`:

- `Put` — store claims for a token under a `bound.Duration` ttl, read against the security contract's `TokenTtlRule`: an absent ttl and one under a second are refused naming the ttl, and `bound.UnlimitedDuration()` stores with no expiry, ended only by `Delete`, `DeleteByUser` or `RevokeBefore`. The user identifier must be valid UTF-8, refused at every door that writes or matches it.
- `Lookup` — resolve claims for a token string.
- `Delete` — revoke a single token.
- `RevokeBefore(userIdentifier, deviceIdentifier, instant)` — publish a revocation boundary. Every token of that user, or of that one device, issued before the instant stops resolving, including tokens written after the call as long as they were stamped earlier. The boundary only ever moves forward, and the one applied at lookup is the later of the user's and the device's, so a user-wide revocation cannot be undone by a device-scoped one. **This is what ends a user's sessions.**
- `RevocationEpoch(runtime, userIdentifier, deviceIdentifier)` — read the boundary a token would be compared against; the zero instant means nothing has been revoked.

The instants on both sides of that comparison come from application clocks. `TokenStoreConfig.MaximumClockSkew` bounds the window a divergent one opens — it widens the boundary by the stated amount and refuses a stamp further ahead of the verifying node than the same amount — and defaults to zero, so nothing changes unless it is set.

- `DeleteByUser(userIdentifier)` — reclaim the keys and index entries of a user's tokens; it re-reads each indexed member's owner so a recycled token string belonging to another user is never touched. Returns the count removed. This is **cleanup, not revocation**: the walk is an `SSCAN` cursor, which does not promise to return a member added while it is in progress, so a token issued during the call survives it. Use `RevokeBefore` to make tokens unusable and this to reclaim what it made unusable.
- `PurgeExpired` — prune index members whose tokens have expired.

[`TokenStoreConfig`](./token_store.go), every field left out being the default and a value outside its rule panicking at `NewTokenStore`, naming the field:

- `Prefix` — the key prefix, default `melody:token`.
- `CallTimeout` — bounds one round trip of every door: 1s when left out, at least 1ms, unlimited refused. Unbounded, a store that accepts connections but stops answering would hold `Put`, `Delete`, `Lookup` and `RevokeBefore` — each one Lua script, which the client does not retry — for the client's write deadline and hold `DeleteByUser`, `PurgeExpired` and `RevocationEpoch` for good, since the client retries their read-only `SSCAN`, `SCAN` and `HMGET` commands for as long as a context without deadline allows — and melody's http kernel attaches none to a request. The runtime doors keep a request deadline that is tighter than the timeout. The bound is per round trip, so a walk over a large user's index gets one budget per batch.
- `ScanCount` — the batch size of the `SSCAN`/`SCAN` walks: 256 when left out, at least 1, unlimited refused.
- `Clock` — the clock stamping a token's issue instant; nil is the system clock, and `RegisterTokenStoreService` fills it from the container when the config names none.
- `MaximumClockSkew` — the window the paragraph above describes: 0 when left out, which is no tolerance, at least 0, unlimited refused.
- `RevocationEpochRetention` — how long a revocation boundary outlives its instant: 7 days when left out, at least 1s, and `bound.UnlimitedDuration()` keeps it for ever, as the in-memory store does, at the cost of one hash kept per user that ever revoked.

## Nonce guard

[`NewNonceGuard(client, NonceGuardConfig)`](./nonce_guard.go) returns a `*NonceGuard` implementing the security `NonceGuard` contract — the shared replay guard for multi-instance HMAC deployments. `Remember(runtime, nonce, ttl)` takes the ttl as a `bound.Duration` read against the security contract's `NonceTtlRule` — an absent ttl, one under a millisecond and an unlimited one are refused as the error, naming the ttl — and records the nonce with a millisecond expiry in a single atomic round-trip (`SET NX PX` via a Lua script) and reports whether it was already seen, so there is no check-then-set race between instances. Because the recorded nonces live in Redis, a nonce replayed against **any** application instance is detected — something the in-process `security.MemoryNonceGuard` cannot do.

- `NonceGuardConfig.KeyPrefix` — the key prefix; empty is the default `melody:nonce`.
- `NonceGuardConfig.CallTimeout` — bounds one round trip of `Remember`: 1s when left out, at least 1ms, unlimited refused, a value outside the rule panicking at `NewNonceGuard` naming the field. Unbounded, a store that accepts connections but stops answering would hold the record for the client's write deadline — and every authenticator that consults the guard runs on the request path, where melody's http kernel attaches none. A request that already carries a tighter deadline keeps it.

```go
nonceGuard := rueidis.NewNonceGuard(client, rueidis.NonceGuardConfig{})

tokenSource := security.NewHmacTokenSource(security.HmacTokenSourceConfig{
    Secrets:    secrets,
    Apps:       apps,
    NonceGuard: nonceGuard,
})
```

The same guard fits any `security/contract.NonceGuard` slot, for example the TOTP authenticator's `ReplayGuard`.

## Rate limiter

[`NewRateLimiter(client, RateLimiterConfig)`](./rate_limit.go) is a Redis-backed fixed-window limiter shared by every application instance — the distributed drop-in for the in-process limiters of the core `http/middleware` package. It implements both `http/contract.RateLimiter` and `http/contract.RuntimeRateLimiter`, so it goes straight into `middleware.RateLimitMiddleware`: `Allow(key)` bounds its own round trip, `AllowWithRuntime(runtimeInstance, key)` — the method the middleware prefers — caps the request context with the call timeout and reports store failures to the caller, and `Reset(key)` drops one counter best-effort. The counter is incremented atomically in a single Lua round trip, so N instances enforce one shared limit; because the window is fixed rather than sliding, up to twice the limit can pass across a window edge. Keys live under the `melody:rate_limit:` prefix, so the limiter shares a Redis instance with the cache, the lock and the token store without colliding.

```go
limiter := rueidis.NewRateLimiter(client, rueidis.RateLimiterConfig{Limit: 60, Window: time.Minute})

rateLimit := middleware.RateLimitMiddleware(limiter, middleware.RateLimitConfig{})
```

[`RateLimiterConfig`](./rate_limit.go), every field but `Limit` and `Window` being the default when left out, and a value outside its rule panicking at `NewRateLimiter`, naming the field:

- `Limit` — the calls admitted per `Window`, at least 1, required.
- `Window` — the span the calls are counted over, at least 1ms, required, as the in-process `LimiterConfig` has them.
- `KeyPrefix` — the key prefix; empty is the default `melody:rate_limit:`.
- `FailureMode` — `FailureModeClosed` when left out, or `FailureModeOpen`; any other value is refused, so a misspelt mode is not read as closed.
- `OnError` — observes store failures, including the ones `Allow` and `Reset` cannot return.
- `CallTimeout` — bounds one store round trip: 250ms when left out, at least 1ms, unlimited refused.
- `Logger` — the journal a store failure is recorded in when the call carries none: `Reset` and the plain `Allow` take no runtime, so without it, or with a typed nil, their failures reach the emergency logger, standard error. `AllowWithRuntime` records on the request's logger first; an `OnError` observer replaces the record either way.

**The limiter is fail-closed by default.** When Redis cannot be reached, every limited route denies traffic — the right default for login and one-time-password endpoints, where an outage must not lift the limit, but it also means a Redis outage takes those routes down with it. Pick `FailureModeOpen` deliberately for plain traffic shaping, where a store outage must not become an outage of every limited route, and wire `OnError` so the failure is visible either way. Construction panics on a nil client.

## Server-Sent Event backplane

[`NewServerSentEventBackplane(client, hub, ServerSentEventBackplaneConfig)`](./server_sent_event_backplane.go) makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a Redis pub/sub channel — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each broadcast is published to a shared channel tagged with a per-instance random origin; a dedicated subscription forwards the events of other instances into the hub via `DeliverLocal` and skips the echo of its own origin, so nothing is delivered twice. The subscription re-subscribes with bounded backoff after a connection drop. The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

```go
hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
backplane := rueidis.NewServerSentEventBackplane(client, hub, rueidis.ServerSentEventBackplaneConfig{Channel: "myapp:sse"})
defer func() {
    _ = backplane.CloseWithContext(ctx)
}()
```

`NewServerSentEventBackplane` resolves its configuration first and then calls `hub.SetBackplane` itself, so a refused configuration leaves the hub without a backplane and, after construction, `hub.Broadcast(...)` replicates automatically. The Redis client is caller-owned; `CloseWithContext` detaches the backplane and stops the subscription goroutine cleanly, but does not close the client. Delivery is best-effort like Server-Sent Events itself; `hub.BackplaneFailures()` counts broadcasts that could not be published.

[`ServerSentEventBackplaneConfig`](./server_sent_event_backplane.go), every field left out being the default, and a value outside its rule panicking at `NewServerSentEventBackplane`, naming the field:

- `Channel` — the shared pub/sub channel every instance publishes to and subscribes on; empty is the default `melody:sse`. Give each application its own channel when several share a Redis instance.
- `Logger` — a `logging/contract.Logger` for subscription failures and reconnect attempts; nil or a typed nil leaves them silent.
- `Reconnect` — the backoff the subscription re-subscribes with after a connection drop, a [`ReconnectConfig`](./reconnect_config.go): `InitialBackoff` 1s and `MaxBackoff` 30s when left out, both at least 1ms, unlimited refused; `BackoffFactor` 2 when left out, exactly 1 a constant backoff. A factor below 1 or NaN is refused, since it would decay the wait toward zero, and so is an initial backoff above the cap, naming both fields.
- `CallTimeout` — bounds one publish round trip: 1s when left out, at least 1ms, unlimited refused. The caller is typically an http handler broadcasting an event, and its context carries no deadline, so without a bound an unresponsive store holds the request instead of failing it. A publish that runs out of time is counted in `hub.BackplaneFailures()` like any other failed replication — the event reaches the local subscribers but not the other instances — so surface that counter as a metric if cross-instance delivery matters, and raise the timeout for a store whose round trip legitimately exceeds it. The timeout derives from the backplane's own context, so `CloseWithContext` also cancels a publish still in flight.

## Cache backend

Package: [`cache`](./cache). [`cache.NewBackend(ctx, client, BackendConfig)`](./cache/backend.go) wraps a `rueidis.Client` and exposes both the classic methods (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`), which run on `ctx`, nil read as the background context, and ctx-first variants (`GetCtx`, `SetCtx`, …) that propagate caller deadlines/cancellation. [`cache.NewBackendService(client, BackendConfig)`](./cache/backend_service.go) is a container-friendly singleton wrapper implementing the core `cache/contract.Backend`. The `rueidis.Client` is owned by the application, not the backend: `Backend.CloseWithContext` does not close the client, so the same client can be shared with the locker, token store, and server-sent-event backplane without one component tearing it down for the others — close the client once during application shutdown.

`Backend.CloseWithContext` does end the backend: every later operation answers `cache backend is closed` rather than serving over a client whose owner already tore this backend down, and a handle minted by `BackendService.WithContext` reads its owner's flag, so the service's `Close` reaches the per-request handles too. A handle minted by `WithContext` copies its owner's resolved configuration. A refusal of a malformed key names it by its first 128 bytes, the length beside it. `ClearByPrefix` refuses the empty prefix rather than reading it as the whole namespace: a prefix assembled at run time that comes out empty would otherwise wipe everything, which is the one outcome a prefixed delete exists to prevent — `Clear` is the door that means the whole namespace.

[`BackendConfig`](./cache/backend.go), every field left out being the default, and a value outside its rule answered as the error of `NewBackend` and `NewBackendService`, naming the field:

- `Prefix` — the whole of the backend's isolation; empty is the default `melody:cache:`, the same string in every melody application, so give each application and environment sharing a store its own.
- `ScanCount` — the `COUNT` hint of the `SCAN` walk `Clear` and `ClearByPrefix` run: 1000 when left out, at least 1, unlimited refused.
- `DeleteBatch` — how many keys one `DEL` carries: 500 when left out, at least 1, unlimited refused.
- `MaxKeyLength` — the key bound the refusals are measured against, on the key the caller hands in, before the prefix: 1024 when left out, at least 1, unlimited refused.
- `CommandTimeout` — bounds every operation dispatched without a caller context, each `SCAN` and `DEL` round trip of a `Clear` or a `ClearByPrefix` on its own, so the size of a namespace is not capped by it: 1s when left out, at least 1ms, unlimited refused. Unbounded, a request-path read against a store that accepts connections but stops answering would be retried for as long as the context allows and hang the handler. The ctx-first variants run on the caller's context alone.

### BackendFromRuntime

Helper: [`cache.BackendFromRuntime`](./cache/backend_service.go)

Returns a per-request `*Backend` handle bound to the runtime's context, minted through `BackendService.WithContext`, so its operations carry the request's deadline and its owner's `CloseWithContext` reaches it. Despite carrying no `Must` in its name, it panics when the service is absent or mistyped — the signature has no error slot to answer through, so treat it as the Must door it is:

```go
package main

func main() {
    scopedBackend := rueidiscache.BackendFromRuntime(runtimeInstance, cache.ServiceCacheBackend)
    scopedBackend.Get("my-key")
}
```

## Plug-and-play registration

Each capability has a one-call registration helper that binds it to a canonical service name, so handlers resolve it through the matching `*MustFromResolver` helper:

```go
rueidis.RegisterClientService(registrar, client)                                                   /* rueidis.ServiceClient, "service.rueidis.client" */
rueidis.RegisterLockerService(registrar, client, rueidis.LockerConfig{})                           /* core lock.ServiceLocker */
rueidis.RegisterTokenStoreService(registrar, client, rueidis.TokenStoreConfig{})                   /* rueidis.ServiceTokenStore, "service.rueidis.token_store" */
rueidiscache.RegisterBackendService(registrar, client, rueidiscache.BackendConfig{Prefix: "app:"}) /* core cache.ServiceCacheBackend */
```

`RegisterClientService`, `RegisterLockerService` and `RegisterTokenStoreService` live in [`service_resolver.go`](./service_resolver.go); the locker and token store doors take their config and resolve it at the call, so a value outside its rule, or a nil client, fails the boot rather than the first resolve; [`RegisterBackendService`](./cache/service_resolver.go) lives in [`cache/service_resolver.go`](./cache/service_resolver.go) and resolves its `BackendConfig` at the call the same way, and the [`cache.ModuleConfig.Backend`](./cache/module.go) field hands the config through the module, as [`ModuleConfig.Locker`](./module.go) and [`ModuleConfig.TokenStore`](./module.go) hand theirs.

Or bundle them as self-registering application modules — `RegisterModule` registers the client service and, opt-in, the locker and revocable token store (and the cache backend), instead of calling each helper by hand. A role with no client, or a role's configuration with the role off, is refused at boot naming the field, and so is a cache module handed a `Backend` configuration with no client:

```go
app.RegisterModule(rueidis.NewModule(rueidis.ModuleConfig{Client: client, AsLocker: true, AsTokenStore: true}))
app.RegisterModule(rueidiscache.NewModule(rueidiscache.ModuleConfig{Client: client, Backend: rueidiscache.BackendConfig{Prefix: "app:"}}))
```
