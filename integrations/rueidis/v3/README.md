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

### Connection retry

[`WithRetryConfig`](./provider.go) makes `Open` re-dial a **transient** failure — a Redis that is not accepting connections yet — with capped exponential backoff, so a cold-start race between containers does not hard-fail the boot. It is **opt-in**: without it, `Open` makes a single attempt. A non-transient error is never retried and is returned immediately.

```go
provider := rueidis.NewProvider(
    rueidis.WithRetryConfig(rueidis.DefaultRetryConfig()),
)
```

[`RetryConfig`](./retry_config.go) fields, each resolved independently against [`DefaultRetryConfig`](./retry_config.go):

| Field               | Default | Notes                                                                                                    |
|---------------------|---------|----------------------------------------------------------------------------------------------------------|
| `MaxAttempts`       | `3`     | Total attempts, not extra ones. `0` resolves to the default.                                             |
| `InitialDelay`      | `500ms` | Delay before the second attempt. A non-positive value resolves to the default.                            |
| `MaxDelay`          | `5s`    | Cap on the grown delay. A non-positive value resolves to the default.                                    |
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
* `DeleteByUser(userIdentifier)` — revoke every token currently owned by a user; it re-reads each indexed member's owner so a recycled token string belonging to another user is never revoked. Returns the count removed.
* `PurgeExpired` — prune index members whose tokens have expired.

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
* [`WithServerSentEventBackplaneReconnectConfig(config)`](./reconnect_config.go) — the backoff the subscription re-subscribes with after a connection drop, as a [`ReconnectConfig`](./reconnect_config.go) (`InitialBackoff` 1s, `MaxBackoff` 30s, `BackoffFactor` 2.0 by [`DefaultReconnectConfig`](./reconnect_config.go)). Unset fields keep their default, a factor below 1 is refused, and an initial backoff above the cap is clamped onto it.
* [`WithServerSentEventBackplaneCallTimeout(timeout)`](./server_sent_event_backplane.go) — bounds one publish round trip, default 1s. The caller is typically an http handler broadcasting an event, and its context carries no deadline, so without a bound an unresponsive store holds the request instead of failing it. A publish that runs out of time is counted in `hub.BackplaneFailures()` like any other failed replication — the event reaches the local subscribers but not the other instances — so surface that counter as a metric if cross-instance delivery matters, and raise the timeout for a store whose round trip legitimately exceeds it. A non-positive value falls back to the default. The timeout derives from the backplane's own context, so `Close` also cancels a publish still in flight.

## Cache backend

Package: [`cache`](./cache). [`cache.NewBackend`](./cache/backend.go) wraps a `rueidis.Client` and exposes both the classic methods (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`) and ctx-first variants (`GetCtx`, `SetCtx`, …) that propagate caller deadlines/cancellation. [`cache.NewBackendService`](./cache/backend_service.go) is a container-friendly singleton wrapper implementing the core `cache/contract.Backend`. The `rueidis.Client` is owned by the application, not the backend: `Backend.Close` does not close the client, so the same client can be shared with the locker, token store, and server-sent-event backplane without one component tearing it down for the others — close the client once during application shutdown.

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
