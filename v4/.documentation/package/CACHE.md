# CACHE

The [`cache`](../../cache) package provides Melody’s cache abstraction: contracts for cache access, a default in-memory backend, JSON serialization, a manager that composes backend+serializer, and a `Remember` helper with stampede protection.

## Scope

- Package: [`cache/`](../../cache)
- Subpackage: [`cache/contract/`](../../cache/contract)

## Subpackages

- [`cache/contract`](../../cache/contract)\
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

- `cache.ServiceCache` (`service.cache`)\
  The cache service ([`cache/contract.Cache`](../../cache/contract/cache.go)).

- `cache.ServiceCacheBackend` (`service.cache.backend`)\
  The backend service ([`cache/contract.Backend`](../../cache/contract/backend.go)).

- `cache.ServiceCacheSerializer` (`service.cache.serializer`)\
  The serializer service ([`cache/contract.Serializer`](../../cache/contract/serializer.go)).

### Default backend configuration (in-memory)

Melody’s default wiring uses [`cache.NewInMemoryBackend`](../../cache/in_memory.go), built from an `InMemoryBackendConfig` whose zero value is the default:

```go
package main

import (
    "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/clock"
)

func main() {
    inMemoryBackend := cache.NewInMemoryBackend(
        cache.InMemoryBackendConfig{
            Clock: clock.NewSystemClock(),
        },
    )
    defer inMemoryBackend.Close()
}
```

The fields mean:

- `MaxItems`\
  The `bound.Count` of entries retained: 10,000 when left out, at least 1, `bound.UnlimitedCount()` lifting the ceiling. A value below the minimum panics at construction, naming the field.

- `CleanupInterval`\
  The `bound.Duration` of how often the backend scans and removes expired entries: 1m when left out, at least 1ms, unlimited refused, so a lapsed entry is always collected.

- `Clock`\
  A `clock/contract.Clock` used for expiration calculations; nil is the system clock.

When the backend reaches its ceiling, eviction probes a bounded tail of the recency list — eight entries — for an expired victim first, and otherwise evicts the least-recently- **promoted** entry (see [`cache/in_memory.go`](../../cache/in_memory.go)). Recency is kept at a coarse grain on purpose: a hit refreshes an atomic access mark under the read lock, and the entry moves to the front of the list at most once per second per key, so the read path does not pay the exclusive lock on every hit and the eviction order is right to within that interval rather than to the last read.

### The default backend is bounded

Melody registers that backend itself only when the application registered no `cache.ServiceCacheBackend` before boot, and it registers it exactly as written above: the 10,000-entry ceiling, and no expiry of its own, so an entry lapses only when the write that stored it carried a ttl. A cache keyed on anything request-derived — a user id, a url, a search term — therefore stops growing at the ceiling, the least recently promoted entries making room for the new ones. An application that wants another ceiling, or none, registers the backend itself, before boot.

Arming the bounds means two separate things:

- the **item ceiling** is the `MaxItems` field, so it is chosen by registering the backend yourself, before boot;
- the **expiry** is the ttl carried by each write — `Set`, `SetMultiple` and [`Remember`](../../cache/remember.go) all take one, as a `bound.Duration` read through [`cache/contract.ResolveTtl`](../../cache/contract/ttl.go), so every backend refuses the same ttls with the same answer: an absent ttl is refused, a value is at least 1s, and `bound.UnlimitedDuration()` stores an entry that never lapses. `Remember` judges the ttl before it reads the cache, so a refused ttl never runs the callback. One boundary instant separates the backends: the in-memory backend treats the exact expiry instant itself as lapsed, while a redis-backed entry is served until strictly past it — a window of at most one millisecond, observable only by a read landing on the precise instant. Redis takes the ttl in whole milliseconds, so a fractional millisecond is truncated. `CleanupInterval` is the *sweep interval*: how often lapsed entries are collected, not a lifetime applied to anything.

The [Usage](#usage) example below registers `cache.NewInMemoryBackend(cache.InMemoryBackendConfig{MaxItems: bound.Of(10000), CleanupInterval: bound.Of(10 * time.Second), Clock: clockInstance})` under `cache.ServiceCacheBackend` before boot, through [`Application.RegisterService`](../../application/application_container.go).

Two hazards belong to what is stored rather than to how long it lives, and neither has a guard the framework can install for you:

- the default serializer writes a bare json document with no schema discriminant, so a value written by one release decodes cleanly in the next — a field added since simply reads as the zero value, with no decoding error to notice and no version to compare — and with an unlimited ttl nothing ever lapses to heal it. Where the shape of a cached value can change between releases, carry a version inside the value or move the cache key with the shape;
- a redis backend's isolation is entirely its key prefix, and the shipped default is the same string in every melody application, while the client's `SelectDb` defaults to `0`. Two applications on one store with the defaults share a namespace: a `Get` answers what the other wrote under the same name — decodable, so served as a hit rather than treated as a miss — and a `Clear` from either empties the other's entries. Give each application, and each environment sharing a store, its own prefix.

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

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

const userProfileCacheKey = "example.userProfile:42"

type userProfileMap map[string]any

func registerCacheOverrides(
    register func(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
),
) {
    register(
        cache.ServiceCacheBackend,
        func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
            clockInstance := clock.ClockMustFromResolver(
                resolver,
            )

            return cache.NewInMemoryBackend(
                cache.InMemoryBackendConfig{
                    MaxItems:        bound.Of(10000),
                    CleanupInterval: bound.Of(10 * time.Second),
                    Clock:           clockInstance,
                },
            ), nil
        },
        containercontract.RegisterOptions{},
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
                cache.ManagerConfig{},
            ), nil
        },
        containercontract.RegisterOptions{},
    )
}

func loadUserProfile(
    ctx context.Context,
    runtimeInstance runtimecontract.Runtime,
) (userProfileMap, error) {
    cacheInstance := cache.CacheMustFromContainer(
        runtimeInstance.Container(),
    )

    value, rememberErr := cache.Remember(
        ctx,
        cacheInstance,
        userProfileCacheKey,
        cache.RememberConfig{
            Ttl: bound.Of(30 * time.Minute),
        },
        func(ctx context.Context) (any, error) {
            _ = ctx

            return userProfileMap{
                "id":   int64(42),
                "name": "demo",
            }, nil
        },
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
  the next major. The round-trip runs BEFORE the store: a value the serializer encodes but cannot decode — the JSON serializer has no nesting ceiling on the way in and one of ten thousand on the way out — is refused without being written, so the key stays empty and the caller gets the refusal once per call; stored first, it was read back as a typed miss on every later call, recomputed, rewritten and refused again, a loop no ttl ended.
- `NewManager` builds a manager that does **not** own its backend: `Close` leaves the backend open, which is what the container path needs — both are registered services and the container closes each one itself. `ManagerConfig{OwnsBackend: true}` keeps the cascade for the caller that builds both by hand.
- A closed `InMemoryBackend` refuses every operation with `cache backend is closed`; `Close` stays idempotent. The key grammar is part of the [`Backend`](../../cache/contract/backend.go) contract: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and a malformed key is refused on every operation by both this backend and the `rueidis` backend, with the same message and in the same order, at the same limit unless the `rueidis` backend's `WithMaxKeyLength` moves its own, and both echo the key cut to 128 bytes.
- `Remember` coalesces concurrent callers per **cache instance, key and cancelability** — the instance is told apart by its pointer. Two managers over one backend are two coalescing units, and a value-kind `Cache` implementation gets no coalescing at all (every caller runs its own callback); implement `Cache` on a pointer receiver to coalesce.
- Every caller of one flight owns its answer: the first takes the value the flight produced and every later one a copy made through the cache's `StoredValueNormalizer`, a `Manager`'s serializer round trip, so no two callers share a map or a slice. A stored value the leader cannot copy is read again from the store by every caller after the first, as a hit outside a flight reads it, and a caller that finds the entry gone by then, or no longer decoding, computes and stores as a miss outside a flight does; a computed value it cannot copy is refused. A bare backend that is no `StoredValueNormalizer` has no copy to make, and the callers of one flight share its one value.
- The zero `RememberConfig` but for its required `Ttl` is the default: stampede protection on, a 30s wait, a **cancelable** flight. A `WaitTimeout` of `bound.Of(time.Duration(0))` means "no waiting": a result already memoized by the in-flight call is returned, anything still computing answers with the timeout error; `bound.UnlimitedDuration()` waits on the flight and the context alone.
- Under the **cancelable** default the flight is canceled when its last waiter leaves, the next caller of the key replacing the canceled flight with a fresh leader, so a hung callback does not own its key. `Detached: true` is the **detached** flight: a leader whose waiters have all left keeps running with its final error written into an in-flight record nothing reads anymore, and a callback that never returns pins its single-flight entry for the life of the process: a later detached caller of the key joins it and waits until its own `WaitTimeout` or context ends, never past them, and a cancelable caller does not join it at all. Give long callbacks their own deadlines either way. The two settings compose one way to know: under the cancelable default a `WaitTimeout` **shorter than the callback** never stores — the lone waiter times out, the flight loses its last waiter and is canceled, the callback that honours its context returns the cancellation, and the next call leads a fresh flight to the same end, so the key is never populated. A bounded wait on a slow callback is paired with `Detached: true`, which lets the
  computation finish and store for the callers after.
- The context, `Remember`'s first parameter, ties the **wait** to the request that opened it, and nothing else; a nil one is refused. A caller that serves an http request passes `request.HttpRequest().Context()` — the melody `Request` carries no `Context()` of its own — so a client that disconnected releases the goroutine that served it, with its scope, its session and everything else the request reached, before the 30s wait ends. The context ends that one caller's wait with `cache remember wait canceled by the caller context`, carrying `context.Canceled` or `context.DeadlineExceeded` as the cause; it never ends the computation, which belongs to the leader and is awaited by everyone coalesced onto it. What cancels the computation is still the last waiter leaving, and only on the cancelable flight, the default. An answer already memoized when the wait begins is handed over whatever the context says. Where the **callback** is what should stop, give it its own deadline: the context it receives
  is the flight's, not the caller's — except on the uncoalesced path (`StampedeProtectionDisabled`, or a value-kind `Cache`), where there is no flight and the callback runs under the caller's context.
- `Manager.Increment` and `Manager.Decrement` are backend-native, so a distributed backend keeps them atomic and the count is stored as decimal text rather than through the serializer. Read such a key with [`Manager.GetCounter`](../../cache/manager.go), never with `Get`, and do not mix `Set` onto the same key. See [`cache/manager.go`](../../cache/manager.go).
- `Remember` uses a single-flight mechanism when stampede protection is enabled (default). See [`cache/remember.go`](../../cache/remember.go).
- `Remember` groups in-flight calls by cache instance, key, and cancelability (cancelable callers are isolated from non-cancelable callers). See [`cache/remember.go`](../../cache/remember.go).
- A cancelable in-flight call whose waiters have all timed out is abandoned: a caller that joins afterwards does not inherit the cancellation error — it starts a fresh computation. See [`cache/remember.go`](../../cache/remember.go).
- [`InMemoryBackend`](../../cache/in_memory.go) owns a cleanup goroutine whose lifetime ends when [`Close`](../../cache/in_memory.go) is called; callers must `Close` the backend — or a `Manager` built over it with `ManagerConfig{OwnsBackend: true}` — to stop it; there is no finalizer fallback, and a manager that does not own its backend does not do it for you.

## Userland API

### Contracts (`cache/contract`)

#### Types

- **Cache** ([`cache/contract/cache.go`](../../cache/contract/cache.go))

```go
package main

type Cache interface {
    Get(key string) (any, bool, error)
    Set(key string, value any, ttl bound.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Clear() error

    Many(keys []string) (map[string]any, error)
    SetMultiple(items map[string]any, ttl bound.Duration) error
    DeleteMultiple(keys []string) error

    Increment(key string, delta int64) (int64, error)
    Decrement(key string, delta int64) (int64, error)

    Close() error
}
```

- **Backend** ([`cache/contract/backend.go`](../../cache/contract/backend.go))
- **TtlRule** and **ResolveTtl** ([`cache/contract/ttl.go`](../../cache/contract/ttl.go)) — the reading every backend gives the ttl of a write: absent refused, at least 1s, unlimited answered as 0, no expiry
- **Serializer** ([`cache/contract/serializer.go`](../../cache/contract/serializer.go))

### Types

- **cache.Manager** ([`cache/manager.go`](../../cache/manager.go))
    - [`NormalizeStoredValue(value any) (any, error)`](../../cache/manager.go) — the shape a value stored through this manager reads back as: one serializer round-trip, run locally with no backend involved. `Remember` consults it so the computing call and the cached calls answer one shape — without it a callback's `int` came back `float64` and its struct came back a map, but only from the second call on. The door is [`cache/contract.StoredValueNormalizer`](../../cache/contract/stored_value_normalizer.go), asked of the `Cache` value `Remember` was handed: a decorator over the manager implements it too, forwarding to the decorated manager, or `Remember` through the decorator answers the two shapes again
- **cache.InMemoryBackend** ([`cache/in_memory.go`](../../cache/in_memory.go))
- **cache.JsonSerializer** ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- **cache.InMemoryBackendConfig** ([`cache/in_memory.go`](../../cache/in_memory.go)) — `MaxItems`, `CleanupInterval` and `Clock`, the zero value the default
- **cache.ManagerConfig** ([`cache/manager.go`](../../cache/manager.go)) — `OwnsBackend`, off by default
- **cache.RememberConfig** ([`cache/remember.go`](../../cache/remember.go)) — `Ttl` required, `StampedeProtectionDisabled`, `WaitTimeout` (30s, 0 no wait, unlimited) and `Detached`
- **cache.DeserializationError** ([`cache/deserialization_error.go`](../../cache/deserialization_error.go)) — the typed error a read answers when the payload does not deserialize; `Remember` treats it as a miss.
- **cache.Item** ([`cache/item.go`](../../cache/item.go)) — the entry representation `InMemoryBackend` keeps internally. Carries key, payload, creation/expiration timestamps, last-access time, and hit count; `cachecontract.Backend.Get` itself returns the raw payload bytes, not an `Item`.

### Constructors

- `cache.NewManager(backend, serializer, config) *cache.Manager` ([`cache/manager.go`](../../cache/manager.go)) — the zero config does not own the backend, which the container path wants; `OwnsBackend` cascades Close into the backend, for the caller that builds both by hand.
- `cache.NewInMemoryBackend(config) *cache.InMemoryBackend` ([`cache/in_memory.go`](../../cache/in_memory.go))
- `cache.NewDeserializationError(keys, causeErr) *cache.DeserializationError` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
- `cache.NewJsonSerializer() cachecontract.Serializer` ([`cache/json_serializer.go`](../../cache/json_serializer.go))
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

- `cache.Remember(ctx, cacheInstance, key, config, callback) (any, error)` ([`cache/remember.go`](../../cache/remember.go))

#### Errors

- `cache.IsDeserializationError(err) bool` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
