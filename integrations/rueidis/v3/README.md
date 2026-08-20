# Rueidis integration (v3)

Redis-backed building blocks for Melody v3, built on [`rueidis`](https://github.com/redis/rueidis):

* a [`Provider`](./provider.go) that opens a `rueidis.Client` from connection parameters;
* a distributed [`Locker`](./lock.go) implementing the core `lock/contract.Locker`;
* a [`RedisTokenStore`](./token_store.go) implementing the security `RevocableTokenStore`;
* a [`NonceGuard`](./nonce_guard.go) implementing the security `NonceGuard` replay guard;
* a [`RateLimiter`](./rate_limit.go) implementing the HTTP `RateLimiter` / `RuntimeRateLimiter` contracts across instances;
* a [`ServerSentEventBackplane`](./server_sent_event_backplane.go) that fans Server-Sent Events across instances;
* a Redis [`cache`](./cache) backend implementing the core `cache/contract.Backend`.

Import path: `github.com/precision-soft/melody/integrations/rueidis/v3`

## Provider

[`NewProvider`](./provider.go) builds a [`Provider`](./provider.go) from optional options ([`WithClientConfig`](./provider.go), [`WithTimeoutConfig`](./provider.go), [`WithRetryConfig`](./provider.go)). `Open` takes a [`ConnectionParameters`](./connection_parameters.go) (address, user, password) and returns a `rueidis.Client`; a comma-separated address list is used as multiple init addresses.

```go
provider := rueidis.NewProvider()

client, err := provider.Open(rueidis.NewConnectionParameters(address, user, password))
if err != nil {
    // connection error
}
```

Optional configuration:

* [`ClientConfig`](./client_config.go) — client name, DB selection, TLS, client-side cache toggle, ping-on-start.
* [`TimeoutConfig`](./timeout_config.go) — connect / command timeouts.
* [`RetryConfig`](./retry_config.go) — the initial-connection retry (see below).

**A configuration is taken whole or not at all.** An absent one is replaced by [`DefaultClientConfig`](./client_config.go) or [`DefaultTimeoutConfig`](./timeout_config.go) entirely; a supplied one is used as it stands, with no field-by-field fill-in. So a partial literal is not "the defaults plus my change": every field left at its zero value is what the provider gets, which turns `PingOnStart` off and turns `DisableCache` off, switching client-side caching on where the shipped default keeps it off — a subsystem with its own memory budget and RESP3 requirements. The blast radius is bounded, measured: the library normalizes a zero dial timeout to its own five seconds, and a dead address still refuses eagerly at client creation even with the ping off. Start from the two constructors above and change what you mean to change.

The provider is handed the connection **values**, not the configuration keys they came from, so it knows no configuration key and names no credential of its own — this package carries no credential-marking door. Arming the framework's redaction is the application's call, through the parameter registrar's `RegisterSecretParameter` for a parameter the application declares, or `MarkParameterSecret` for one melody registered from the `.env` artifacts. The mark propagates to every parameter whose template reads the secret, so a dsn assembled from the credential is redacted with it and `debug:parameters` masks the password in a process that never dials.

Every refusal the provider writes carries the deadlines that governed the attempt — the connect timeout and the dial timeout, the latter reported as the value that actually governed rather than the one configured, since a zero or negative `DialTimeout` runs under the library's own five seconds. The password is never part of that record.

### Owning the client

`rueidis.Client.Close` returns nothing, so the raw client cannot join the container's ordered teardown — which closes what answers `Close() error` and nothing else. [`NewConnection`](./connection.go) wraps it in that shape: register the [`Connection`](./connection.go) as the service that owns the client and resolve the client through it, and the container closes the one owner, once, in dependency order. Every value in this package that could close a client it merely borrows — the cache backend, the rate limiter — deliberately declines to.

### Connection retry

[`WithRetryConfig`](./provider.go) makes `Open` re-dial a **transient** failure — a Redis that is not accepting connections yet — with capped exponential backoff, so a cold-start race between containers does not hard-fail the boot. It is **opt-in**: without it, `Open` makes a single attempt. A non-transient error is never retried and is returned immediately.

```go
provider := rueidis.NewProvider(
    rueidis.WithRetryConfig(rueidis.DefaultRetryConfig()),
)
```

[`RetryConfig`](./retry_config.go) fields, each resolved independently against [`DefaultRetryConfig`](./retry_config.go):

| Field               | Default | Notes                                                                                                                               |
|---------------------|---------|-------------------------------------------------------------------------------------------------------------------------------------|
| `MaxAttempts`       | `3`     | Total attempts, not extra ones. `0` resolves to the default.                                                                        |
| `InitialDelay`      | `500ms` | Delay before the second attempt. A non-positive value resolves to the default.                                                      |
| `MaxDelay`          | `5s`    | Cap on the grown delay. A non-positive value resolves to the default.                                                               |
| `BackoffMultiplier` | `2.0`   | Growth per attempt. Anything not at least `1` — including `NaN` — resolves to the default; exactly `1` is a valid constant backoff. |

Because each field is defaulted on its own, a partially filled `RetryConfig` cannot collapse the backoff into a re-dial storm. Retries are reported through the framework logger: a warning per retry, an error when the attempts run out, and an info line when a later attempt succeeds.

### Defaults

| Config          | Field              | Default |
|-----------------|--------------------|---------|
| `TimeoutConfig` | `ConnectTimeout`   | `3s`    |
| `TimeoutConfig` | `CommandTimeout`   | `3s`    |
| `ClientConfig`  | `ClientName`       | empty   |
| `ClientConfig`  | `SelectDb`         | `0`     |
| `ClientConfig`  | `DisableCache`     | `true`  |
| `ClientConfig`  | `TlsConfig`        | `nil`   |
| `ClientConfig`  | `PingOnStart`      | `true`  |
| `ClientConfig`  | `DialTimeout`      | `5s`    |
| `ClientConfig`  | `ConnWriteTimeout` | `5s`    |

## Distributed lock

[`NewLocker(client)`](./lock.go) returns a `lock/contract.Locker`; `CreateLock(name, ttl)` returns a `lock/contract.Lock` backed by a Redis key with a TTL. `Acquire` is a non-blocking try (returns `(false, nil)` when another holder owns the key), `Release` deletes the key only if still owned, and `Refresh` extends the TTL, returning a "lock is no longer held" error if the lease was lost.

## Token store

[`NewTokenStore(client, options...)`](./token_store.go) returns a `*RedisTokenStore` implementing the security `RevocableTokenStore`:

* `Put` / `PutWithTtl` — store claims for a token (TTL defaults to the token's own expiry).
* `Lookup` — resolve claims for a token string.
* `Delete` — revoke a single token.
* `RevokeBefore(userIdentifier, deviceIdentifier, instant)` — publish a revocation boundary. Every token of that user, or of that one device, issued before the instant stops resolving, including tokens written after the call as long as they were stamped earlier. The boundary only ever moves forward, and the one applied at lookup is the later of the user's and the device's, so a user-wide revocation cannot be undone by a device-scoped one. **This is what ends a user's sessions.**
* `RevocationEpoch(runtime, userIdentifier, deviceIdentifier)` — read the boundary a token would be compared against; the zero instant means nothing has been revoked.

The instants on both sides of that comparison come from application clocks. `WithTokenStoreMaximumClockSkew` bounds the window a divergent one opens — it widens the boundary by the stated amount and refuses a stamp further ahead of the verifying node than the same amount — and defaults to zero, so nothing changes unless it is set.

* `DeleteByUser(userIdentifier)` — reclaim the keys and index entries of a user's tokens; it re-reads each indexed member's owner so a recycled token string belonging to another user is never touched. Returns the count removed. This is **cleanup, not revocation**: the walk is an `SSCAN` cursor, which does not promise to return a member added while it is in progress, so a token issued during the call survives it. Use `RevokeBefore` to make tokens unusable and this to reclaim what it made unusable.
* `PurgeExpired` — prune index members whose tokens have expired.

Options:

* [`WithTokenStorePrefix(prefix)`](./token_store.go) — the key prefix, default `melody:token`.
* [`WithTokenStoreMaximumClockSkew(skew)`](./token_store.go) and [`WithRevocationEpochRetention(retention)`](./token_store.go) — the revocation-boundary tunables the paragraphs above describe.

## Nonce guard

[`NewNonceGuard(client)`](./nonce_guard.go) returns a `*NonceGuard` implementing the security `NonceGuard` contract — the shared replay guard for multi-instance HMAC deployments. `Remember(runtime, nonce, ttl)` records the nonce with a millisecond expiry in a single atomic round-trip (`SET NX PX` via a Lua script) and reports whether it was already seen, so there is no check-then-set race between instances. Because the recorded nonces live in Redis, a nonce replayed against **any** application instance is detected — something the in-process `security.MemoryNonceGuard` cannot do. [`NewNonceGuardWithPrefix(client, keyPrefix)`](./nonce_guard.go) overrides the default `melody:nonce` key prefix.

```go
nonceGuard := rueidis.NewNonceGuard(client)

tokenSource := security.NewHmacTokenSource(security.HmacTokenSourceConfig{
    Secrets:    secrets,
    Apps:       apps,
    NonceGuard: nonceGuard,
})
```

The same guard fits any `security/contract.NonceGuard` slot, for example the TOTP authenticator's `ReplayGuard`.

## Rate limiter

[`NewRateLimiter(client, limit, window, options...)`](./rate_limit.go) is a Redis-backed fixed-window limiter shared by every application instance — the distributed drop-in for the in-process limiters of the core `http/middleware` package. It implements both `http/contract.RateLimiter` and `http/contract.RuntimeRateLimiter`, so it goes straight into a `middleware.RateLimitConfig`: `Allow(key)` bounds its own round trip, `AllowWithRuntime(runtimeInstance, key)` — the method the middleware prefers — caps the request context with the call timeout and reports store failures to the caller, and `Reset(key)` drops one counter best-effort. The counter is incremented atomically in a single Lua round trip, so N instances enforce one shared limit; because the window is fixed rather than sliding, up to twice the limit can pass across a window edge. Keys live under the `melody:rate_limit:` prefix, so the limiter shares a Redis instance with the cache, the lock and the token store without colliding.

```go
limiter := rueidis.NewRateLimiter(client, 60, time.Minute)

rateLimit := middleware.RateLimitMiddleware(middleware.NewRateLimitConfig(limiter, nil, nil))
```

Options:

* [`WithRateLimiterKeyPrefix(prefix)`](./rate_limit.go) — the key prefix, default `melody:rate_limit:`.
* [`WithRateLimiterFailureMode(mode)`](./rate_limit.go) — `FailureModeClosed` (the default) or `FailureModeOpen`.
* [`WithRateLimiterOnError(handler)`](./rate_limit.go) — observes store failures, including the ones `Allow` and `Reset` cannot return.
* [`WithRateLimiterCallTimeout(timeout)`](./rate_limit.go) — bounds one store round trip, default 250 milliseconds. A non-positive value falls back to the default.

**The limiter is fail-closed by default.** When Redis cannot be reached, every limited route denies traffic — the right default for login and one-time-password endpoints, where an outage must not lift the limit, but it also means a Redis outage takes those routes down with it. Pick `FailureModeOpen` deliberately for plain traffic shaping, where a store outage must not become an outage of every limited route, and wire `WithRateLimiterOnError` so the failure is visible either way. Construction panics on a nil client, a non-positive limit, or a non-positive window.

## Server-Sent Event backplane

[`NewServerSentEventBackplane(client, hub, options...)`](./server_sent_event_backplane.go) makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a Redis pub/sub channel — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each broadcast is published to a shared channel tagged with a per-instance random origin; a dedicated subscription forwards the events of other instances into the hub via `DeliverLocal` and skips the echo of its own origin, so nothing is delivered twice. The subscription re-subscribes with bounded backoff after a connection drop. The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

```go
hub := melodyhttp.NewServerSentEventHub()
backplane := rueidis.NewServerSentEventBackplane(client, hub, rueidis.WithServerSentEventBackplaneChannel("myapp:sse"))
defer backplane.Close()
```

`NewServerSentEventBackplane` calls `hub.SetBackplane` itself, so after construction `hub.Broadcast(...)` replicates automatically. The Redis client is caller-owned; `Close` detaches the backplane and stops the subscription goroutine cleanly, but does not close the client. Delivery is best-effort like Server-Sent Events itself; `hub.BackplaneFailures()` counts broadcasts that could not be published.

Options:

* [`WithServerSentEventBackplaneChannel(channel)`](./server_sent_event_backplane.go) — the shared pub/sub channel every instance publishes to and subscribes on, default `melody:sse`. Give each application its own channel when several share a Redis instance.
* [`WithServerSentEventBackplaneLogger(logger)`](./server_sent_event_backplane.go) — a `logging/contract.Logger` for subscription failures and reconnect attempts; without it those stay silent.
* [`WithServerSentEventBackplaneReconnectConfig(config)`](./server_sent_event_backplane.go) — the backoff the subscription re-subscribes with after a connection drop, as a [`ReconnectConfig`](./reconnect_config.go) (`InitialBackoff` 1s, `MaxBackoff` 30s, `BackoffFactor` 2.0 by [`DefaultReconnectConfig`](./reconnect_config.go)). Unset fields keep their default, a factor below 1 is refused, and an initial backoff above the cap is clamped onto it.
* [`WithServerSentEventBackplaneCallTimeout(timeout)`](./server_sent_event_backplane.go) — bounds one publish round trip, default 1s. The caller is typically an http handler broadcasting an event, and its context carries no deadline, so without a bound an unresponsive store holds the request instead of failing it. A publish that runs out of time is counted in `hub.BackplaneFailures()` like any other failed replication — the event reaches the local subscribers but not the other instances — so surface that counter as a metric if cross-instance delivery matters, and raise the timeout for a store whose round trip legitimately exceeds it. A non-positive value falls back to the default. The timeout derives from the backplane's own context, so `Close` also cancels a publish still in flight.

## Cache backend

Package: [`cache`](./cache). [`cache.NewBackend`](./cache/backend.go) wraps a `rueidis.Client` and exposes both the classic methods (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`) and ctx-first variants (`GetCtx`, `SetCtx`, …) that propagate caller deadlines/cancellation. [`cache.NewBackendService`](./cache/backend_service.go) is a container-friendly singleton wrapper implementing the core `cache/contract.Backend`. The `rueidis.Client` is owned by the application, not the backend: `Backend.Close` does not close the client, so the same client can be shared with the locker, token store, and server-sent-event backplane without one component tearing it down for the others — close the client once during application shutdown.

`Backend.Close` does end the backend: every later operation answers `cache backend is closed` rather than serving over a client whose owner already tore this backend down, and a handle minted by `BackendService.WithContext` reads its owner's flag, so the service's `Close` reaches the per-request handles too. [`WithCommandTimeout`](./cache/backend.go) bounds the half of the contract that carries no caller context — without it a request-path read against a store that accepts connections but stops answering hangs the handler; a non-positive value reads as unbounded. [`WithMaxKeyLength`](./cache/backend.go) moves the key bound the refusals are measured against. `ClearByPrefix` refuses the empty prefix rather than reading it as the whole namespace: a prefix assembled at run time that comes out empty would otherwise wipe everything, which is the one outcome a prefixed delete exists to prevent — `Clear` is the door that means the whole namespace.

### BackendFromRuntime

Helper: [`cache.BackendFromRuntime`](./cache/backend_service.go)

Returns a per-request `*Backend` handle bound to the runtime's context, minted through `BackendService.WithContext`, so its operations carry the request's deadline and its owner's `Close` reaches it. Despite carrying no `Must` in its name, it panics when the service is absent or mistyped — the signature has no error slot to answer through, so treat it as the Must door it is:

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
rueidis.RegisterClientService(registrar, client)               /* rueidis.ServiceClient, "service.rueidis.client" */
rueidis.RegisterLockerService(registrar, client)               /* core lock.ServiceLocker */
rueidis.RegisterTokenStoreService(registrar, client)           /* rueidis.ServiceTokenStore, "service.rueidis.token_store" */
rueidiscache.RegisterBackendService(registrar, client, "app:") /* core cache.ServiceCacheBackend */
```

`RegisterClientService`, `RegisterLockerService`, and `RegisterTokenStoreService` live in [`service_resolver.go`](./service_resolver.go); `RegisterBackendService` lives in [`cache/service_resolver.go`](./cache/service_resolver.go).

Or bundle them as self-registering application modules — `RegisterModule` registers the client service and, opt-in, the locker and revocable token store (and the cache backend), instead of calling each helper by hand:

```go
app.RegisterModule(rueidis.NewModule(rueidis.ModuleConfig{Client: client, AsLocker: true, AsTokenStore: true}))
app.RegisterModule(rueidiscache.NewModule(rueidiscache.ModuleConfig{Client: client, Prefix: "app:"}))
```
