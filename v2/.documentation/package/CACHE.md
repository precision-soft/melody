# CACHE

The [`cache`](../../cache) package provides Melody’s cache abstraction: contracts for cache access, a default in-memory backend, JSON serialization, a manager that composes backend+serializer, and a `Remember` helper with stampede protection.

## Scope

- Package: [`cache/`](../../cache)
- Subpackage: [`cache/contract/`](../../cache/contract)

## Subpackages

- [`cache/contract`](../../cache/contract)  
  Public contracts for cache, backend, and serializer.

## Responsibilities

- Provide a unified cache interface ([`cache/contract.Cache`](../../cache/contract/cache.go)) used across the framework.
- Offer [`cache.Manager`](../../cache/manager.go), which composes a backend and a serializer to store arbitrary values.
- Provide [`cache.InMemoryBackend`](../../cache/in_memory.go) as the default backend (LRU + TTL + cleanup loop).
- Provide [`cache.Remember`](../../cache/remember.go) as a cache-aside helper with optional stampede protection.

## Configuration

Cache wiring is performed via container services. You may override the default backend/serializer/cache by registering the corresponding services **before boot**.

### Service ids

Defined in [`cache/service_resolver.go`](../../cache/service_resolver.go):

- `cache.ServiceCache` (`service.cache`)  
  The cache service ([`cache/contract.Cache`](../../cache/contract/cache.go)).

- `cache.ServiceCacheBackend` (`service.cache.backend`)  
  The backend service ([`cache/contract.Backend`](../../cache/contract/backend.go)).

- `cache.ServiceCacheSerializer` (`service.cache.serializer`)  
  The serializer service ([`cache/contract.Serializer`](../../cache/contract/serializer.go)).

### Default backend configuration (in-memory)

Melody’s default wiring uses [`cache.NewInMemoryBackend`](../../cache/in_memory.go):

```go
package main

import (
	"github.com/precision-soft/melody/v2/cache"
	"github.com/precision-soft/melody/v2/clock"
)

func main() {
	clockInstance := clock.NewSystemClock()

	inMemoryBackend := cache.NewInMemoryBackend(
		0,
		0,
		clockInstance,
	)
}
```

The arguments mean:

- `maxItems`  
  Maximum number of cache entries retained. A non-positive value disables the size limit.

- `cleanupInterval`  
  How often the backend scans and removes expired entries. A non-positive value defaults to `time.Minute`.

- `clockInstance`  
  A `clock/contract.Clock` used for expiration calculations.

When `maxItems` is enabled and the backend reaches capacity, eviction is deterministic: it evicts an expired entry from the least-recently-used end first, otherwise it evicts the least-recently-used entry (see [`cache/in_memory.go`](../../cache/in_memory.go)).

## Container integration

The cache package exposes retrieval helpers for the three services (see [`cache/service_resolver.go`](../../cache/service_resolver.go)):

- `cache.CacheMustFromContainer(container)`
- `cache.CacheBackendMustFromContainer(container)`
- `cache.CacheSerializerMustFromContainer(container)`

Use the resolver variants when you already have a `container/contract.Resolver`:

- `cache.CacheMustFromResolver(resolver)`
- `cache.CacheBackendMustFromResolver(resolver)`
- `cache.CacheSerializerMustFromResolver(resolver)`

## Usage

The example below demonstrates a typical Melody flow:

1. Override the cache backend and cache service **before boot** (application wiring).
2. Retrieve the cache from the runtime container.
3. Use `Remember` for cache-aside reads with stampede protection.

This example uses a `map[string]any` payload because the default JSON serializer deserializes into generic Go values (see [`cache/json_serializer.go`](../../cache/json_serializer.go)).

```go
package main

import (
	"context"
	"time"

	"github.com/precision-soft/melody/v2/cache"
	cachecontract "github.com/precision-soft/melody/v2/cache/contract"
	"github.com/precision-soft/melody/v2/clock"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const userProfileCacheKey = "example.userProfile:42"

type userProfileMap map[string]any

func registerCacheOverrides(
	register func(
	serviceName string,
	provider any,
	options ...any,
),
) {
	register(
		cache.ServiceCacheBackend,
		func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
			clockInstance := clock.ClockMustFromResolver(
				resolver,
			)

			return cache.NewInMemoryBackend(
				10000,
				10*time.Second,
				clockInstance,
			), nil
		},
	)

	register(
		cache.ServiceCache,
		func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
			backend := cache.CacheBackendMustFromResolver(
				resolver,
			)
			serializerInstance := cache.CacheSerializerMustFromResolver(
				resolver,
			)

			return cache.NewManager(
				backend,
				serializerInstance,
			), nil
		},
	)
}

func loadUserProfile(
	runtimeInstance runtimecontract.Runtime,
) (userProfileMap, error) {
	cacheInstance := cache.CacheMustFromContainer(
		runtimeInstance.Container(),
	)

	value, rememberErr := cache.Remember(
		cacheInstance,
		userProfileCacheKey,
		30*time.Minute,
		func(ctx context.Context) (any, error) {
			_ = ctx

			return userProfileMap{
				"id":   int64(42),
				"name": "demo",
			}, nil
		},
		nil,
	)
	if nil != rememberErr {
		return nil, rememberErr
	}

	profile, ok := value.(map[string]any)
	if false == ok {
		return nil, exception.NewError(
			"cached value has unexpected type",
			map[string]any{
				"key": userProfileCacheKey,
			},
			nil,
		)
	}

	return userProfileMap(profile), nil
}
```

## Footguns & caveats

- `Manager.Get` returns `exists == false` when deserialization fails (and returns the deserialization error). See [`cache/manager.go`](../../cache/manager.go).
- `Remember` uses a single-flight mechanism when stampede protection is enabled (default). See [`cache/remember.go`](../../cache/remember.go).
- `Remember` groups in-flight calls by cache instance, key, and cancelability (cancelable callers are isolated from non-cancelable callers). See [`cache/remember.go`](../../cache/remember.go).

## Userland API

### Contracts (`cache/contract`)

#### Types

- **Cache** ([`cache/contract/cache.go`](../../cache/contract/cache.go))

```go
package main

type Cache interface {
	Get(key string) (any, bool, error)
	Set(key string, value any, ttl time.Duration) error
	Delete(key string) error
	Has(key string) (bool, error)
	Clear() error

	Many(keys []string) (map[string]any, error)
	SetMultiple(items map[string]any, ttl time.Duration) error
	DeleteMultiple(keys []string) error

	Increment(key string, delta int64) (int64, error)
	Decrement(key string, delta int64) (int64, error)

	Close() error
}
```

- **Backend** ([`cache/contract/backend.go`](../../cache/contract/backend.go))
- **Serializer** ([`cache/contract/serializer.go`](../../cache/contract/serializer.go))

### Types

- **cache.Manager** ([`cache/manager.go`](../../cache/manager.go))
- **cache.InMemoryBackend** ([`cache/in_memory.go`](../../cache/in_memory.go))
- **cache.JsonSerializer** ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- **cache.RememberOption** ([`cache/remember.go`](../../cache/remember.go))

### Constructors

- `cache.NewManager(backend, serializer) *cache.Manager` ([`cache/manager.go`](../../cache/manager.go))
- `cache.NewInMemoryBackend(maxItems, cleanupInterval, clockInstance) *cache.InMemoryBackend` ([`cache/in_memory.go`](../../cache/in_memory.go))
- `cache.NewJsonSerializer() cachecontract.Serializer` ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- `cache.NewDefaultRememberOption() *cache.RememberOption` ([`cache/remember.go`](../../cache/remember.go))

### Retrieval helpers

- `cache.CacheMustFromContainer(container) cachecontract.Cache`
- `cache.CacheBackendMustFromContainer(container) cachecontract.Backend`
- `cache.CacheSerializerMustFromContainer(container) cachecontract.Serializer`

- `cache.CacheMustFromResolver(resolver) cachecontract.Cache`
- `cache.CacheBackendMustFromResolver(resolver) cachecontract.Backend`
- `cache.CacheSerializerMustFromResolver(resolver) cachecontract.Serializer`

### Functions

#### Cache-aside

- `cache.Remember(cacheInstance, key, ttl, callback, option) (any, error)` ([`cache/remember.go`](../../cache/remember.go))
