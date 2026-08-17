# Rueidis integration

This integration provides:

* A small `Provider` that opens a `rueidis.Client` from Melody config parameters.
* A Redis-backed distributed rate limiter for the core `http/middleware` rate-limit middleware.
* A Redis-backed Melody cache backend implemented on top of Rueidis.

## Provider

Entry point: [`NewProvider`](./provider.go)

The provider reads parameters (address, username, password) using the names you pass to the constructor. If you provide a comma-separated list of addresses, each item is used as an init address.

Optional configuration:

* [`ClientConfig`](./client_config.go) (client name, DB selection, TLS, disable client-side cache, ping on start, and the two deadlines that actually reach the client: `DialTimeout` and `ConnWriteTimeout`)
* [`TimeoutConfig`](./timeout_config.go) — **boot only**. `ConnectTimeout` and `CommandTimeout` bound the provider's own ping round trips; neither is passed into `rueidis.ClientOption`, so ordinary commands are not bounded by them. The network deadlines a running application answers to are the two `ClientConfig` fields above.

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

`BackendService` is a singleton wrapper intended for service container registration. It holds a `Backend` (built with `context.Background()`) and implements [`cache/contract.Backend`](../../../v2/cache/contract/backend.go) by forwarding each call to the underlying `Backend`.

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
	"github.com/precision-soft/melody/v2/application"
	"github.com/precision-soft/melody/v2/cache"
	cachecontract "github.com/precision-soft/melody/v2/cache/contract"
	"github.com/precision-soft/melody/v2/config"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
	rueidisintegration "github.com/precision-soft/melody/integrations/rueidis/v2"
	rueidiscache "github.com/precision-soft/melody/integrations/rueidis/v2/cache"
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
