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
	"github.com/precision-soft/melody/v3/cache"
	"github.com/precision-soft/melody/v3/clock"
)

func main() {
	clockInstance := clock.NewSystemClock()

	inMemoryBackend := cache.NewInMemoryBackend(
		0,
		0,
		clockInstance,
	)
	defer inMemoryBackend.Close()
}
```

The arguments mean:

- `maxItems`  
  Maximum number of cache entries retained. Zero disables the size limit; a negative value panics at construction, so a bound computed wrong cannot silently disarm eviction.

- `cleanupInterval`  
  How often the backend scans and removes expired entries. A non-positive value defaults to `time.Minute`.

- `clockInstance`  
  A `clock/contract.Clock` used for expiration calculations.

When `maxItems` is enabled and the backend reaches capacity, eviction probes a bounded tail of the recency list — eight entries — for an expired victim first, and otherwise evicts the least-recently- **promoted** entry (see [`cache/in_memory.go`](../../cache/in_memory.go)). Recency is kept at a coarse grain on purpose: a hit refreshes an atomic access mark under the read lock, and the entry moves to the front of the list at most once per second per key, so the read path does not pay the exclusive lock on every hit and the eviction order is right to within that interval rather than to the last read.

### The default backend is unbounded, and boot says so

Melody registers that backend itself only when the application registered no `cache.ServiceCacheBackend` before boot, and it registers it exactly as written above: `maxItems` `0`, which disables the size limit, and a backend that attaches no expiry of its own, so an entry lapses only when the write that stored it carried a positive ttl. Neither dimension is armed, and that is deliberate: a ceiling melody picked would evict an application's entries behind its back, and an expiry melody picked would drop them early. What the choice costs is therefore reported rather than decided, once, at boot ([`application/application_http.go`](../../application/application_http.go)):

```
the default in-memory cache backend carries no item ceiling, so a key cached without a ttl is kept until this process exits and nothing evicts it under memory pressure; register `service.cache.backend` with cache.NewInMemoryBackend(maxItems, cleanupInterval, clock) for a bounded one, or with a shared backend
```

What it means: whether an entry ever leaves the map is decided entirely at the call site. A key cached with a positive ttl is reclaimed by the sweep; a key cached without one stays for the life of the process, and with no ceiling nothing is ever evicted to make room for it. So a cache keyed on anything request-derived — a user id, a url, a search term — and written without a ttl grows with the traffic and nothing reclaims it. The constructor's second argument sets how often the sweep runs, not how long an entry lives.

The warning is raised on the **http serving path only**. A command builds its map, runs and exits with it, so a cli invocation has nothing to accumulate and nothing to act on; warning it anyway would only teach it to ignore the warnings it can act on.

Arming the bounds means two separate things:

- the **item ceiling** is the `maxItems` constructor argument, so it is chosen by registering the backend yourself, before boot;
- the **expiry** is the ttl carried by each write — `Set`, `SetMultiple` and [`Remember`](../../cache/remember.go) all take one. A ttl of zero stores an entry that never lapses; a negative ttl is refused with an error, because a ttl computed from an already-passed deadline means "as good as expired", not "eternal". One boundary instant separates the backends: the in-memory backend treats the exact expiry instant itself as lapsed, while a redis-backed entry is served until strictly past it — a window of at most one millisecond, observable only by a read landing on the precise instant, and the single place the otherwise identical backend grammars diverge. The constructor's second argument, `cleanupInterval`, is the *sweep interval*: how often lapsed entries are collected, defaulting to `time.Minute` when non-positive, not a lifetime applied to anything.

The [Usage](#usage) example below does exactly that: it registers `cache.NewInMemoryBackend(10000, 10*time.Second, clockInstance)` under `cache.ServiceCacheBackend` before boot, through [`Application.RegisterService`](../../application/application_container.go).

Two hazards belong to what is stored rather than to how long it lives, and neither has a guard the framework can install for you:

- the default serializer writes a bare json document with no schema discriminant, so a value written by one release decodes cleanly in the next — a field added since simply reads as the zero value, with no decoding error to notice and no version to compare — and with a ttl of zero nothing ever lapses to heal it. Where the shape of a cached value can change between releases, carry a version inside the value or move the cache key with the shape;
- a redis backend's isolation is entirely its key prefix, and the shipped default is the same string in every melody application, while the client's `SelectDb` defaults to `0`. Two applications on one store with the defaults share a namespace: a `Get` answers what the other wrote under the same name — decodable, so served as a hit rather than treated as a miss — and a `Clear` from either empties the other's entries. Give each application, and each environment sharing a store, its own prefix.

Registering any backend of your own silences the warning — a bounded in-memory one, or a shared one such as the `rueidis` integration — because it is armed only when melody had to supply the backend itself.

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

	"github.com/precision-soft/melody/v3/cache"
	cachecontract "github.com/precision-soft/melody/v3/cache/contract"
	"github.com/precision-soft/melody/v3/clock"
	containercontract "github.com/precision-soft/melody/v3/container/contract"
	"github.com/precision-soft/melody/v3/exception"
	runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const userProfileCacheKey = "example.userProfile:42"

type userProfileMap map[string]any

func registerCacheOverrides(
	register func(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
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

- `Manager.Get` returns `exists == false` when deserialization fails, and the error is a typed [`DeserializationError`](../../cache/deserialization_error.go) naming the key. `Remember` treats that type as a miss: the callback recomputes and overwrites the corrupt payload, so the key heals; every other error keeps meaning the cache itself failed. A `Cache` implementation of your own inherits the self-healing by wrapping its deserialization failures with `cache.NewDeserializationError`.
- `Manager.Many` leaves a corrupt entry out of the result the way an absent key is, and returns the good values **beside** a `DeserializationError` naming the corrupt keys in requested order — check the error even when the map is non-empty.
- Values round-trip through the serializer, and the default `NewJsonSerializer` decodes every JSON number into a `float64`: an integer beyond 2^53 comes back a **different integer**, with no decoding error to notice — the same hazard [`SESSION.md`](SESSION.md) records for its file storage. `Remember` runs the round-trip on its own computing call too, so the miss answers the same shape every hit does, which means the loss is uniform rather than only on the second call: a callback that returns `int64(1<<60)` reads back changed on the cold path as well. The `int64(42)` above is safe because it is under 2^53; a snowflake id, a `bigint` key, a minor-unit amount or a `UnixNano()` is not. Carry a version or a string inside the value, or key such a value so it never decodes through JSON. Every major's default serializer has this shape, v3's included — its `NewJsonSerializer` decodes through the same `json.Unmarshal` into `any` — so carrying the value differently is the remedy, not waiting for
  the next major.
- `NewManager` builds a manager that does **not** own its backend: `Close` leaves the backend open, which is what the container path needs — both are registered services and the container closes each one itself. `NewManagerOwningBackend` keeps the cascade for the caller that builds both by hand.
- A closed `InMemoryBackend` refuses every operation with `cache backend is closed`; `Close` stays idempotent. The key grammar is part of the [`Backend`](../../cache/contract/backend.go) contract: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and a malformed key is refused identically on every operation by both this backend and the `rueidis` backend.
- `Remember` coalesces concurrent callers per **cache instance, key and cancelability** — the instance is told apart by its pointer. Two managers over one backend are two coalescing units, and a value-kind `Cache` implementation gets no coalescing at all (every caller runs its own callback); implement `Cache` on a pointer receiver to coalesce.
- The zero-value `&RememberOption{}` reads as `NewDefaultRememberOption()` — stampede protection on, unbounded wait, **non-cancelable**: exactly the configuration under which the pinned-leader hazard below is reachable, so the hazard is the default's, not an exotic option's. A deliberate protection-off option is built through the constructor. A `waitTimeout` of zero means "no waiting": a result already memoized by the in-flight call is returned, anything still computing answers with the timeout error.
- A `Remember` leader whose waiters have all timed out keeps running detached: its final error is written into an in-flight record nothing reads anymore, and a **non-cancelable** callback that never returns pins its single-flight entry (and every future waiter of that key) for the life of the process. Give long callbacks their own deadlines, or opt into `WithCancelable(true)` so an abandoned flight is canceled and replaced.
- `WithContext` ties the **wait** to the request that opened it, and nothing else. A caller that serves an http request should pass `request.HttpRequest().Context()` — the melody `Request` carries no `Context()` of its own: without one the shipped default is an unbounded wait, so a client that disconnected keeps the goroutine that served it parked for as long as the callback takes — and with the goroutine, its scope, its session and everything else the request reached. The context ends that one caller's wait with `cache remember wait canceled by the caller context`, carrying `context.Canceled` or `context.DeadlineExceeded` as the cause; it never ends the computation, which belongs to the leader and is awaited by everyone coalesced onto it. What cancels the computation is still the last waiter leaving, and only under `WithCancelable(true)`. An answer already memoized when the wait begins is handed over whatever the context says. Where the **callback** is what should stop, give the
  callback its own deadline: the context it receives is the flight's, not the caller's — except on the uncoalesced path (`WithStampedeProtectionEnabled(false)`, or a value-kind `Cache`), where there is no flight and the callback runs under the caller's context.
- The default backend melody wires when the application registers none carries **no item ceiling**, so a key written without a ttl is kept until the process exits and nothing evicts it under memory pressure; the http path warns about it once at boot. See [The default backend is unbounded, and boot says so](#the-default-backend-is-unbounded-and-boot-says-so).
- `Manager.Increment` and `Manager.Decrement` are backend-native, so a distributed backend keeps them atomic and the count is stored as decimal text rather than through the serializer. Read such a key with [`Manager.GetCounter`](../../cache/manager.go), never with `Get`, and do not mix `Set` onto the same key. See [`cache/manager.go`](../../cache/manager.go).
- `Remember` uses a single-flight mechanism when stampede protection is enabled (default). See [`cache/remember.go`](../../cache/remember.go).
- `Remember` groups in-flight calls by cache instance, key, and cancelability (cancelable callers are isolated from non-cancelable callers). See [`cache/remember.go`](../../cache/remember.go).
- A cancelable in-flight call whose waiters have all timed out is abandoned: a caller that joins afterwards does not inherit the cancellation error — it starts a fresh computation. See [`cache/remember.go`](../../cache/remember.go).
- [`InMemoryBackend`](../../cache/in_memory.go) owns a cleanup goroutine whose lifetime ends when [`Close`](../../cache/in_memory.go) is called; callers must `Close` the backend — or a `Manager` built with `NewManagerOwningBackend` over it — to stop it; there is no finalizer fallback, and a manager built with `NewManager` does not do it for you.

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
    - [`NormalizeStoredValue(value any) (any, error)`](../../cache/manager.go) — the shape a value stored through this manager reads back as: one serializer round-trip, run locally with no backend involved. `Remember` consults it so the computing call and the cached calls answer one shape — without it a callback's `int` came back `float64` and its struct came back a map, but only from the second call on
- **cache.InMemoryBackend** ([`cache/in_memory.go`](../../cache/in_memory.go))
- **cache.JsonSerializer** ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- **cache.RememberOption** ([`cache/remember.go`](../../cache/remember.go))
    - [`WithStampedeProtectionEnabled(enableStampedeProtection bool)`](../../cache/remember.go) / [`EnableStampedeProtection()`](../../cache/remember.go)
    - [`WithWaitTimeout(waitTimeout time.Duration)`](../../cache/remember.go) / [`WaitTimeout()`](../../cache/remember.go) — the only door onto the wait the footguns below discuss. A deliberate zero means no-wait and is told apart from the field left unspoken, which answers the default
    - [`WithCancelable(isCancelable bool)`](../../cache/remember.go) / [`IsCancelable()`](../../cache/remember.go)
    - [`WithContext(callerContext context.Context)`](../../cache/remember.go) / [`Context()`](../../cache/remember.go) — ties the **wait** to the request that opened it, and nothing else
- **cache.DeserializationError** ([`cache/deserialization_error.go`](../../cache/deserialization_error.go)) — the typed error a read answers when the payload does not deserialize; `Remember` treats it as a miss.
- **cache.Item** ([`cache/item.go`](../../cache/item.go)) — the entry representation `InMemoryBackend` keeps internally. Carries key, payload, creation/expiration timestamps, last-access time, and hit count; `cachecontract.Backend.Get` itself returns the raw payload bytes, not an `Item`.

### Constructors

- `cache.NewManager(backend, serializer) *cache.Manager` ([`cache/manager.go`](../../cache/manager.go)) — the manager does not own the backend; the container path wants this one.
- `cache.NewManagerOwningBackend(backend, serializer) *cache.Manager` ([`cache/manager.go`](../../cache/manager.go)) — Close cascades into the backend, for the caller that builds both by hand.
- `cache.NewInMemoryBackend(maxItems, cleanupInterval, clockInstance) *cache.InMemoryBackend` ([`cache/in_memory.go`](../../cache/in_memory.go))
- `cache.NewDeserializationError(keys, causeErr) *cache.DeserializationError` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
- `cache.NewJsonSerializer() cachecontract.Serializer` ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- `cache.NewDefaultRememberOption() *cache.RememberOption` ([`cache/remember.go`](../../cache/remember.go))
- `cache.NewItem(key string, payload []byte, createdAt time.Time, expiresAt *time.Time) *cache.Item` ([`cache/item.go`](../../cache/item.go))

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

#### Errors

- `cache.IsDeserializationError(err) bool` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
