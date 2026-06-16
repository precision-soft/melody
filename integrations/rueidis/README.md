# Rueidis integration

This integration provides:

* A small `Provider` that opens a `rueidis.Client` from Melody config parameters.
* A Redis-backed Melody cache backend implemented on top of Rueidis.
* A Redis-backed revocable token store for the core `security` package.

## Provider

Entry point: [`NewProvider`](./provider.go)

The provider reads parameters (address, username, password) using the names you pass to the constructor.
If you provide a comma-separated list of addresses, each item is used as an init address.

Optional configuration:

* [`ClientConfig`](./client_config.go) (client name, DB selection, TLS, disable client-side cache, ping on start)
* [`TimeoutConfig`](./timeout_config.go) (connect / command timeouts)

## Cache backend

Package: [`cache`](./cache)

### Backend

Entry point: [`cache.NewBackend`](./cache/backend.go)

`Backend` wraps a `rueidis.Client`. It exposes two parallel surfaces:

* the classic methods — `Get(key)`, `Set(key, payload, ttl)`, `Delete(key)`, `Has(key)`, `Clear()`, `ClearByPrefix(prefix)`, `Many(keys)`, `SetMultiple(items, ttl)`, `DeleteMultiple(keys)`, `Increment(key, delta)`, `Decrement(key, delta)` — which reuse the `ctx` captured by `NewBackend`. These are supported but legacy; new code should prefer the ctx-first surface below.
* the ctx-first methods — `GetCtx(ctx, key)`, `SetCtx(ctx, key, payload, ttl)`, `DeleteCtx(ctx, key)`, `HasCtx(ctx, key)`, `ClearCtx(ctx)`, `ClearByPrefixCtx(ctx, prefix)`, `ManyCtx(ctx, keys)`, `SetMultipleCtx(ctx, items, ttl)`, `DeleteMultipleCtx(ctx, keys)`, `IncrementCtx(ctx, key, delta)`, `DecrementCtx(ctx, key, delta)` — which take a caller-supplied context so deadlines and cancellation propagate end-to-end.

Classic (supported, legacy) pattern:

```go
package main

func main() {
	backend, _ := rueidiscache.NewBackend(client, ctx, "my-prefix:", 0, 0)
	backend.Get("my-key")
	backend.ClearByPrefix("accessToken:")
}
```

Ctx-first (preferred for new code) pattern:

```go
package main

func main() {
	backend, _ := rueidiscache.NewBackend(client, context.Background(), "my-prefix:", 0, 0)
	backend.GetCtx(ctx, "my-key")
	backend.ClearByPrefixCtx(ctx, "accessToken:")
}
```

### BackendService

Entry point: [`cache.NewBackendService`](./cache/backend_service.go)

`BackendService` is a singleton wrapper intended for service container registration.
It holds a `Backend` (built with `context.Background()`) and implements [`cache/contract.Backend`](../../cache/contract/backend.go) by forwarding each call to the underlying `Backend`.

Use `WithContext` to obtain a `*Backend` bound to a specific context. From there you can use either surface:

```go
package main

func main() {
	// Classic (supported, legacy):
	scopedBackend := backendService.WithContext(runtimeInstance.Context())
	scopedBackend.Get("my-key")
	scopedBackend.ClearByPrefix("accessToken:")

	// Ctx-first (preferred) — no rebind needed:
	backendService.Backend().GetCtx(runtimeInstance.Context(), "my-key")
	backendService.Backend().ClearByPrefixCtx(runtimeInstance.Context(), "accessToken:")
}
```

### BackendFromRuntime

Helper: [`cache.BackendFromRuntime`](./cache/backend_service.go)

Returns a `*Backend` bound to the runtime request context, following the same
pattern as Melody's repository `FromRuntime` helpers:

```go
package main

func main() {
	scopedBackend := rueidiscache.BackendFromRuntime(runtimeInstance, ServiceCacheRueidis)
	scopedBackend.Get("my-key")
}
```

## Usage example

### Service registration

Register the Redis client, cache backend service, and Melody's generic cache backend
in your application bootstrap:

```go
package main

import (
	"github.com/precision-soft/melody/application"
	"github.com/precision-soft/melody/cache"
	cachecontract "github.com/precision-soft/melody/cache/contract"
	"github.com/precision-soft/melody/config"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	rueidisintegration "github.com/precision-soft/melody/integrations/rueidis"
	rueidiscache "github.com/precision-soft/melody/integrations/rueidis/cache"
	"github.com/redis/rueidis"
)

const (
	ServiceRedisClient  = "service.redis.client"
	ServiceCacheRueidis = "service.cache.rueidis"
)

func RegisterCacheServices(app *application.Application) {
	app.RegisterService(
		ServiceRedisClient,
		func(resolver containercontract.Resolver) (rueidis.Client, error) {
			provider := rueidisintegration.NewProvider(
				"CACHE_REDIS_ADDRESS",
				"CACHE_REDIS_USER",
				"CACHE_REDIS_PASSWORD",
			)

			return provider.Open(resolver)
		},
	)

	app.RegisterService(
		ServiceCacheRueidis,
		func(resolver containercontract.Resolver) (*rueidiscache.BackendService, error) {
			configuration := config.ConfigMustFromResolver(resolver)
			client := container.MustFromResolver[rueidis.Client](resolver, ServiceRedisClient)
			prefix := configuration.MustGet("CACHE_REDIS_PREFIX").String()

			return rueidiscache.NewBackendService(client, prefix, 0, 0)
		},
	)

	app.RegisterService(
		cache.ServiceCacheBackend,
		func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
			return container.MustFromResolver[*rueidiscache.BackendService](
				resolver, ServiceCacheRueidis,
			), nil
		},
	)
}
```

### Plug-and-play registration (v3)

In v3 (`github.com/precision-soft/melody/integrations/rueidis/v3`) the integration ships the registration helpers, so the manual wiring above collapses to a few calls. Register the Redis client once, then the lock, the revocable token store, and the cache backend against their core service names:

```go
rueidis.RegisterClientService(registrar, client)
rueidis.RegisterLockerService(registrar, client)        // registers lock.ServiceLocker
rueidis.RegisterTokenStoreService(registrar, client)    // registers rueidis.ServiceTokenStore
cache.RegisterBackendService(registrar, client, "app")  // registers cache.ServiceCacheBackend
```

Services then resolve them with `lock.LockerMustFromResolver`, `rueidis.TokenStoreMustFromResolver`, and `cache.CacheBackendMustFromResolver` (the backend that `RegisterBackendService` registers under `cache.ServiceCacheBackend`). The Server-Sent Events backplane self-registers on the hub through `rueidis.NewServerSentEventBackplane(client, hub)`.

### Request-scoped context

Create a thin helper that binds the service name, then use it in handlers:

```go
package main

func BackendFromRuntime(runtimeInstance runtimecontract.Runtime) *rueidiscache.Backend {
	return rueidiscache.BackendFromRuntime(runtimeInstance, ServiceCacheRueidis)
}
```

```go
package main

func (instance *MyController) Handle(
	runtimeInstance runtimecontract.Runtime,
) (httpcontract.Response, error) {
	scopedBackend := BackendFromRuntime(runtimeInstance)

	// Classic (supported, legacy):
	payload, found, err := scopedBackend.Get("my-key")

	// Ctx-first (preferred):
	payload, found, err = scopedBackend.GetCtx(runtimeInstance.Context(), "my-key")
}
```

## Token store

Entry point: [`NewTokenStore`](./v3/token_store.go)

A Redis-backed implementation of the core [`security/contract.RevocableTokenStore`](../../v3/security/contract/token_store.go). It is a drop-in replacement for `security.NewInMemoryTokenStore` behind an `OpaqueTokenValidator`, so revocation survives restarts and is shared across instances.

Key schema:

* `<prefix>:token:<token>` — JSON-encoded claims, with `PX` set to the ttl (`PutWithTtl`); `Put` stores it without expiry.
* `<prefix>:user:<userIdentifier>` — a set of the user's token keys, so `DeleteByUser` revokes every token a user holds in one call.

The token string and user identifier are used verbatim as the trailing key segment. Redis keys are a flat namespace, so a `:` inside either value cannot collide across the fixed `:token:`/`:user:` segments nor between two distinct identifiers, and the values are never parsed back out of the key.

`Put`/`Delete`/`DeleteByUser` run Lua so the token key and the user index stay consistent in a single round trip (re-issuing a token to a different user re-indexes it). `DeleteByUser` returns the number of live tokens revoked and drops the set. Redis expires the token keys natively; `PurgeExpired` only reconciles the user index, pruning members whose token key has already expired and returning the count pruned — run it periodically (e.g. from a cron command), not on the hot path.

Context: the context-less mutators (`Put`/`Delete`/`DeleteByUser`/`PurgeExpired`) use a constructor-bound context (`WithTokenStoreContext`, default background); `Lookup` uses the per-request `runtime.Context()`.

```go
package main

import (
	rueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
	"github.com/precision-soft/melody/v3/security"
	securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const ServiceTokenStore = "security.token_store"

func registerTokenStore(builder containercontract.Builder, client redis.Client) {
	builder.Set(ServiceTokenStore, func(runtimeInstance runtimecontract.Runtime) any {
		return rueidis.NewTokenStore(
			client,
			rueidis.WithTokenStorePrefix("myapp:token"),
		)
	})
}
```

Wire the store into an `OpaqueTokenValidator` exactly as you would the in-memory one — the firewall configuration is identical because both satisfy `securitycontract.TokenStore`.

## Server-Sent Events backplane

`NewServerSentEventBackplane(client, hub, ...options)` makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a Redis pub/sub channel — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each broadcast is published to a shared channel (`WithServerSentEventBackplaneChannel`, default `melody:sse`) tagged with a per-instance random origin; a dedicated subscription forwards the events of other instances into the hub via `DeliverLocal` and skips the echo of its own origin, so nothing is delivered twice. The subscription re-subscribes with bounded backoff after a connection drop. The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

```go
hub := melodyhttp.NewServerSentEventHub()
backplane := rueidis.NewServerSentEventBackplane(client, hub, rueidis.WithServerSentEventBackplaneChannel("myapp:sse"))
defer backplane.Close()
```

`NewServerSentEventBackplane` calls `hub.SetBackplane` itself, so after construction `hub.Broadcast(...)` replicates automatically. The Redis client is caller-owned; `Close` stops the subscription but does not close the client. Delivery is best-effort like Server-Sent Events itself; `hub.BackplaneFailures()` counts broadcasts that could not be published.
