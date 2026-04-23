# Rueidis integration

This integration provides:

* A small `Provider` that opens a `rueidis.Client` from Melody config parameters.
* A Redis-backed Melody cache backend implemented on top of Rueidis.

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
It holds a `Backend` (built with `context.Background()`) and implements [`cache/contract.Backend`](../../../cache/contract/backend.go) by forwarding each call to the underlying `Backend`.

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
