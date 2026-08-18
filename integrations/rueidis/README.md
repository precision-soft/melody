# Rueidis integration

This integration provides:

* A small [`Provider`](./provider.go) that opens a `rueidis.Client` from Melody config parameters.
* A Redis-backed distributed [`RateLimiter`](./rate_limit.go) for the core `http/middleware` rate-limit middleware.
* A Redis-backed Melody cache backend implemented on top of Rueidis.
* A Redis-backed revocable token store for the core `security` package (v3 binding only).

## Provider

Entry point: [`NewProvider`](./provider.go)

The provider reads parameters (address, username, password) using the names you pass to the constructor. If you provide a comma-separated list of addresses, each item is used as an init address.

The password parameter is redacted in the framework's introspection output — `debug:parameters` above all — only once something arms the marking. `Open` arms it at the first dial, which covers no process that never resolves the client, so call [`MarkSecretParameters`](./provider.go) at wiring time with the providers you construct: it reads each provider's [`SecretParameterNames`](./provider.go) and marks the parameters through the configuration's own `MarkSecret`, tolerating a resolver that carries no configuration service. This is the arming the bunorm drivers get from their registry at construction; this integration has no registry, so the wiring is the construction site.

Optional configuration:

* [`ClientConfig`](./client_config.go) (client name, DB selection, TLS, disable client-side cache, ping on start, and the two deadlines that actually reach the client: `DialTimeout` and `ConnWriteTimeout`)
* [`TimeoutConfig`](./timeout_config.go) — **boot only**. `ConnectTimeout` and `CommandTimeout` bound the provider's own ping round trips; neither is passed into `rueidis.ClientOption`, so ordinary commands are not bounded by them. The network deadlines a running application answers to are the two `ClientConfig` fields above.

Either configuration reaches the provider through the chainable [`WithClientConfig`](./provider.go) and [`WithTimeoutConfig`](./provider.go) methods on the value `NewProvider` returns, or through [`NewProviderWithConfig`](./provider.go), which takes both beside the parameter names in one call.

**A configuration is taken whole or not at all.** An absent one is replaced by [`DefaultClientConfig`](./client_config.go) or [`DefaultTimeoutConfig`](./timeout_config.go) entirely; a supplied one is used as it stands, with no field-by-field fill-in — unlike the bunorm drivers, which do fill in field by field. So a partial literal is not "the defaults plus my change": every field left at its zero value is what the provider gets, which turns `PingOnStart` off, so a store that cannot be reached is discovered by the first request rather than at boot, and turns `DisableCache` off, which switches client-side caching on where the shipped default keeps it off. Start from the two constructors above and change what you mean to change.

## Rate limiter

Entry point: [`NewRateLimiter`](./rate_limit.go)

A Redis-backed fixed-window limiter shared by every application instance — the distributed drop-in for the in-process limiters of the core `http/middleware` package. It implements both `http/contract.RateLimiter` and `http/contract.RuntimeRateLimiter`, so it goes straight into a `middleware.RateLimitConfig`. The counter is incremented atomically in a single Lua round trip, so N instances enforce one shared limit; because the window is fixed rather than sliding, up to twice the limit can pass across a window edge.

```go
package main

func main() {
	limiter := rueidisintegration.NewRateLimiter(client, 60, time.Minute)

	rateLimit := middleware.RateLimitMiddleware(
		middleware.NewRateLimitConfig(limiter, nil, nil),
	)
}
```

The limiter surface:

* `Allow(key)` — the context-less entry point; it bounds its own round trip with the call timeout.
* `AllowWithRuntime(runtimeInstance, key)` — the entry point the rate-limit middleware prefers; it caps the request context with the call timeout, so a request that already carries a tighter deadline keeps it while a request with no deadline still fails fast, and it reports the store failure alongside the decision.
* `Reset(key)` — drops the counter for one key, best-effort; a store failure reaches the error observer where one was given, and is otherwise written as a `rate limiter store failure` record through the request's logger or the emergency logger, marked already-logged.

Keys live under the `melody:rate_limit:` prefix, so inside one application the limiter shares a Redis instance with the cache — the other namespace this major ships — without colliding. Between two applications it is the opposite: the shipped prefixes are the same strings in every melody application and the client's `SelectDb` defaults to `0`, so two applications on one Redis with the defaults share every namespace — give each one its own prefix.

Optional configuration:

* [`WithRateLimiterKeyPrefix(prefix)`](./rate_limit.go) — the key prefix, default `melody:rate_limit:`.
* [`WithRateLimiterFailureMode(mode)`](./rate_limit.go) — `FailureModeClosed` (the default) or `FailureModeOpen`.
* [`WithRateLimiterOnError(handler)`](./rate_limit.go) — observes store failures, including the ones `Allow` and `Reset` cannot return.
* [`WithRateLimiterCallTimeout(timeout)`](./rate_limit.go) — bounds one store round trip, default 250 milliseconds. A non-positive value falls back to the default.

**The limiter is fail-closed by default.** When Redis cannot be reached, every limited route denies traffic — the right default for login and one-time-password endpoints, where an outage must not lift the limit, but it also means a Redis outage takes those routes down with it. Pick `FailureModeOpen` deliberately for plain traffic shaping, where a store outage must not become an outage of every limited route, and wire `WithRateLimiterOnError` so the failure is visible either way. Construction panics on a nil client, a non-positive limit, or a non-positive window.

## Cache backend

Package: [`cache`](./cache)

### Backend

Entry point: [`cache.NewBackend`](./cache/backend.go), or [`cache.NewBackendWithCommandTimeout`](./cache/backend.go) to bound the half of the surface that carries no context. `NewBackend` leaves those unbounded, which is the case worth knowing: against a store that accepts connections and stops answering, a ctx-less call has no deadline of its own to fall back on. A non-positive timeout reads as unbounded, so the two constructors agree by construction.

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

Entry point: [`cache.NewBackendService`](./cache/backend_service.go), or [`cache.NewBackendServiceWithCommandTimeout`](./cache/backend_service.go) to bound every contract call. The `cache/contract.Backend` methods carry no context at all, so without a bound a read on the request path against an unanswering store hangs the handler; a non-positive timeout is the unbounded behaviour of `NewBackendService`.

`BackendService` is a singleton wrapper intended for service container registration. It holds a `Backend` (built with `context.Background()`) and implements [`cache/contract.Backend`](../../cache/contract/backend.go) by forwarding each call to the underlying `Backend`.

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

Returns a `*Backend` bound to the runtime request context. Despite carrying no `Must` in its name, it panics when the service is absent or mistyped — treat it as the Must door it is:

```go
package main

func main() {
	scopedBackend := rueidiscache.BackendFromRuntime(runtimeInstance, ServiceCacheRueidis)
	scopedBackend.Get("my-key")
}
```

## Usage example

### Service registration

Register the Redis client, cache backend service, and Melody's generic cache backend in your application bootstrap:

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
)

const (
	ServiceRedisClient  = "service.redis.client"
	ServiceCacheRueidis = "service.cache.rueidis"
)

func RegisterCacheServices(app *application.Application) {
	/* the client is registered wrapped in a Connection: rueidis.Client.Close returns nothing, so a
	   bare client can never join the container's ordered teardown and its connections outlive the
	   process's own shutdown. Everything below reaches the client through Connection.Client(). */
	app.RegisterService(
		ServiceRedisClient,
		func(resolver containercontract.Resolver) (*rueidisintegration.Connection, error) {
			provider := rueidisintegration.NewProvider(
				"CACHE_REDIS_ADDRESS",
				"CACHE_REDIS_USER",
				"CACHE_REDIS_PASSWORD",
			)

			client, openErr := provider.Open(resolver)
			if nil != openErr {
				return nil, openErr
			}

			return rueidisintegration.NewConnection(client), nil
		},
	)

	app.RegisterService(
		ServiceCacheRueidis,
		func(resolver containercontract.Resolver) (*rueidiscache.BackendService, error) {
			configuration := config.ConfigMustFromResolver(resolver)
			connection := container.MustFromResolver[*rueidisintegration.Connection](resolver, ServiceRedisClient)
			client := connection.Client()
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

## Token store (v3)

Entry point: [`NewTokenStore`](./v3/token_store.go) — ships in the v3 binding only (`github.com/precision-soft/melody/integrations/rueidis/v3`).

A Redis-backed implementation of the core [`security/contract.RevocableTokenStore`](../../v3/security/contract/token_store.go). It is a drop-in replacement for `security.NewInMemoryTokenStore` behind an `OpaqueTokenValidator`, so revocation survives restarts and is shared across instances.

Key schema — the prefix is wrapped in a Redis Cluster **hash tag**, so every key of one store hashes to the same slot and the Lua scripts below stay single-slot ([`keyspace()`](./v3/token_store.go)):

* `{<prefix>}:token:<token>` — JSON-encoded claims, with `PX` set to the ttl (`PutWithTtl`); `Put` stores it without expiry.
* `{<prefix>}:user:<userIdentifier>` — a set of the user's token keys, so `DeleteByUser` revokes every token a user holds in one call.

Note the braces when writing a `SCAN`/`KEYS` pattern by hand: with the default prefix `melody:token` ([`WithTokenStorePrefix`](./v3/token_store.go) overrides it) the pattern is `{melody:token}:token:*`, not `melody:token:token:*` — the latter matches nothing.

The token string and user identifier are used verbatim as the trailing key segment. Redis keys are a flat namespace, so a `:` inside either value cannot collide across the fixed `:token:`/`:user:` segments nor between two distinct identifiers, and the values are never parsed back out of the key.

`Put`/`Delete`/`DeleteByUser` run Lua so the token key and the user index stay consistent in a single round trip (re-issuing a token to a different user re-indexes it). `DeleteByUser` returns the number of live tokens revoked and drops the set. Redis expires the token keys natively; `PurgeExpired` only reconciles the user index, pruning members whose token key has already expired and returning the count pruned — run it periodically (e.g. from a cron command), not on the hot path.

Context: the context-less mutators (`Put`/`Delete`/`DeleteByUser`/`PurgeExpired`) use a constructor-bound context (`WithTokenStoreContext`, default background); `Lookup` uses the per-request `runtime.Context()`.

```go
package main

import (
	melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
	"github.com/precision-soft/melody/v3/application"
	"github.com/precision-soft/melody/v3/container"
	containercontract "github.com/precision-soft/melody/v3/container/contract"
	"github.com/redis/rueidis"
)

const (
	ServiceRedisClient = "service.redis.client"
	ServiceTokenStore  = "security.token_store"
)

func registerTokenStore(app *application.Application) {
	app.RegisterService(
		ServiceTokenStore,
		func(resolver containercontract.Resolver) (*melodyrueidis.RedisTokenStore, error) {
			client := container.MustFromResolver[rueidis.Client](resolver, ServiceRedisClient)

			return melodyrueidis.NewTokenStore(
				client,
				melodyrueidis.WithTokenStorePrefix("myapp:token"),
			), nil
		},
	)
}
```

Wire the store into an `OpaqueTokenValidator` exactly as you would the in-memory one — the firewall configuration is identical because both satisfy `securitycontract.TokenStore`.

## Server-Sent Events backplane (v3)

Entry point: [`NewServerSentEventBackplane`](./v3/server_sent_event_backplane.go) — ships in the v3 binding only (`github.com/precision-soft/melody/integrations/rueidis/v3`).

`NewServerSentEventBackplane(client, hub, ...options)` makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a Redis pub/sub channel — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each broadcast is published to a shared channel (`WithServerSentEventBackplaneChannel`, default `melody:sse`) tagged with a per-instance random origin; a dedicated subscription forwards the events of other instances into the hub via `DeliverLocal` and skips the echo of its own origin, so nothing is delivered twice. The subscription re-subscribes with bounded backoff after a connection drop. The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

```go
hub := melodyhttp.NewServerSentEventHub()
backplane := rueidis.NewServerSentEventBackplane(client, hub, rueidis.WithServerSentEventBackplaneChannel("myapp:sse"))
defer backplane.Close()
```

`NewServerSentEventBackplane` calls `hub.SetBackplane` itself, so after construction `hub.Broadcast(...)` replicates automatically. The Redis client is caller-owned; `Close` stops the subscription but does not close the client. Delivery is best-effort like Server-Sent Events itself; `hub.BackplaneFailures()` counts broadcasts that could not be published.

`WithServerSentEventBackplaneCallTimeout(d)` bounds one publish round trip (default 1s). The caller is typically an http handler broadcasting an event, and its context carries no deadline, so without a bound an unresponsive store holds the request instead of failing it. A publish that runs out of time is counted in `hub.BackplaneFailures()` like any other failed replication — the event reaches the local subscribers but not the other instances — so surface that counter as a metric if cross-instance delivery matters, and raise the timeout for a store whose round trip legitimately exceeds it. A non-positive value falls back to the default. The timeout derives from the backplane's own context, so `Close` also cancels a publish still in flight.
