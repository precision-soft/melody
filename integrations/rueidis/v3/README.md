# Rueidis integration (v3)

Redis-backed building blocks for Melody v3, built on [`rueidis`](https://github.com/redis/rueidis):

* a [`Provider`](./provider.go) that opens a `rueidis.Client` from connection parameters;
* a distributed [`Locker`](./lock.go) implementing the core `lock/contract.Locker`;
* a [`RedisTokenStore`](./token_store.go) implementing the security `RevocableTokenStore`;
* a [`ServerSentEventBackplane`](./server_sent_event_backplane.go) that fans Server-Sent Events across instances;
* a Redis [`cache`](./cache) backend implementing the core `cache/contract.Backend`.

Import path: `github.com/precision-soft/melody/integrations/rueidis/v3`

## Provider

[`NewProvider`](./provider.go) builds a [`Provider`](./provider.go) from optional options ([`WithClientConfig`](./provider.go), [`WithTimeoutConfig`](./provider.go)). `Open` takes a [`ConnectionParams`](./connection_params.go) (address, user, password) and returns a `rueidis.Client`; a comma-separated address list is used as multiple init addresses.

```go
provider := rueidis.NewProvider()

client, err := provider.Open(rueidis.NewConnectionParams(address, user, password))
if err != nil {
    // connection error
}
```

Optional configuration:

* [`ClientConfig`](./client_config.go) — client name, DB selection, TLS, client-side cache toggle, ping-on-start.
* [`TimeoutConfig`](./timeout_config.go) — connect / command timeouts.

## Distributed lock

[`NewLocker(client)`](./lock.go) returns a `lock/contract.Locker`; `CreateLock(name, ttl)` returns a `lock/contract.Lock` backed by a Redis key with a TTL. `Acquire` is a non-blocking try (returns `(false, nil)` when another holder owns the key), `Release` deletes the key only if still owned, and `Refresh` extends the TTL, returning a "lock is no longer held" error if the lease was lost.

## Token store

[`NewTokenStore(client, options...)`](./token_store.go) returns a `*RedisTokenStore` implementing the security `RevocableTokenStore`:

* `Put` / `PutWithTtl` — store claims for a token (TTL defaults to the token's own expiry).
* `Lookup` — resolve claims for a token string.
* `Delete` — revoke a single token.
* `DeleteByUser(userIdentifier)` — revoke every token currently owned by a user; it re-reads each indexed member's owner so a recycled token string belonging to another user is never revoked. Returns the count removed.
* `PurgeExpired` — prune index members whose tokens have expired.

## Server-Sent Event backplane

[`NewServerSentEventBackplane(client, hub, options...)`](./server_sent_event_backplane.go) bridges the core HTTP `ServerSentEventHub` across instances over Redis pub/sub: `Publish(topic, event)` broadcasts to every subscribed instance, and `Close` detaches the backplane and stops the subscription goroutine cleanly.

## Cache backend

Package: [`cache`](./cache). [`cache.NewBackend`](./cache/backend.go) wraps a `rueidis.Client` and exposes both the classic methods (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`) and ctx-first variants (`GetCtx`, `SetCtx`, …) that propagate caller deadlines/cancellation. [`cache.NewBackendService`](./cache/backend_service.go) is a container-friendly singleton wrapper implementing the core `cache/contract.Backend`. The `rueidis.Client` is owned by the application, not the backend: `Backend.Close` does not close the client, so the same client can be shared with the locker, token store, and server-sent-event backplane without one component tearing it down for the others — close the client once during application shutdown.

## Plug-and-play registration

Each capability has a one-call registration helper that binds it to the canonical core service name, so handlers resolve it through the matching `*MustFromResolver` helper:

```go
rueidis.RegisterClientService(registrar, client)              // service.redis.client
rueidis.RegisterLockerService(registrar, client)              // core lock.ServiceLocker
rueidis.RegisterTokenStoreService(registrar, client)          // security RevocableTokenStore
rueidiscache.RegisterBackendService(registrar, client, "app:") // core cache.ServiceCacheBackend
```

`RegisterClientService`, `RegisterLockerService`, and `RegisterTokenStoreService` live in [`service_resolver.go`](./service_resolver.go); `RegisterBackendService` lives in [`cache/service_resolver.go`](./cache/service_resolver.go).

Or bundle them as self-registering application modules — `RegisterModule` registers the client service and, opt-in, the locker and revocable token store (and the cache backend), instead of calling each helper by hand:

```go
app.RegisterModule(rueidis.NewModule(rueidis.ModuleConfig{Client: client, AsLocker: true, AsTokenStore: true}))
app.RegisterModule(rueidiscache.NewModule(rueidiscache.ModuleConfig{Client: client, Prefix: "app:"}))
```
