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
    "context"

    "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/clock"
)

func main() {
    inMemoryBackend := cache.NewInMemoryBackend(
        cache.InMemoryBackendConfig{
            Clock: clock.NewSystemClock(),
        },
    )
    defer func() {
        _ = inMemoryBackend.CloseWithContext(context.Background())
    }()
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
- a redis backend's isolation is entirely its key prefix, and the shipped default is the same string in every melody application, while the client's `SelectDatabase` defaults to `0`. Two applications on one store with the defaults share a namespace: a `Get` answers what the other wrote under the same name — decodable, so served as a hit rather than treated as a miss — and a `Clear` from either empties the other's entries. Give each application, and each environment sharing a store, its own prefix.

## Container integration

The cache package exposes retrieval helpers for the three services (see [`cache/service_resolver.go`](../../cache/service_resolver.go)); each takes the container as it takes a scope, the form a provider holding a resolver calls:

- `cache.CacheMustFromResolver(ctx, resolver)`
- `cache.CacheBackendMustFromResolver(ctx, resolver)`
- `cache.CacheSerializerMustFromResolver(ctx, resolver)`

Code holding a runtime, a handler, a command, a listener or a message handler, binds the cache to it instead: `cache.FromRuntime(runtimeInstance)` resolves the application's cache through the runtime's scope and answers a `cache.Bound`.

The framework writes into the application's cache itself in one place: the TOTP second factor's failed attempt counters, under `melody:2fa:failed:` (`security.DefaultFailedAttemptKeyPrefix`), each beside a `:until` key holding its window's deadline, so a `Clear` unlocks every account the throttle locked ([SECURITY.md](SECURITY.md#two-factor-authentication-totp)).

## The bound view

Every door of `cachecontract.Cache` takes the runtime it runs under first; that is the contract a manager, a decorator or a fake implements. Userland calls it through `cache.Bound`, a view bound once to its request, run or delivery, whose methods take no runtime:

```go
products := cache.FromRuntime(runtimeInstance)

found, getErr := products.Get(productCacheKey, &product)
deleteErr := products.Delete(productListCacheKey)
views, incrementErr := products.Increment(productViewsCacheKey, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
counted, exists, getCounterErr := products.GetCounter(productViewsCacheKey)
```

- `cache.For(runtimeInstance, cacheInstance)` binds a cache the caller already holds, the form a service writes: it keeps the unbound cache in a field and binds it at the top of each method.
- A view is a value of one runtime: it is never kept in a service, a field or a goroutine that outlives the runtime, and no provider registers one, so the wiring generator refuses a constructor that asks for a `cache.Bound`.
- A call that needs another deadline, or one that must outlive the caller's cancellation, binds again over a derived runtime: `cache.For(runtime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context())), cacheInstance)`.
- Binding and calling through a view allocate nothing of their own; `cache.FromRuntime` costs the by-name resolution, so a function binds once and calls many times.
- `RoundTrip` takes no runtime and is reached through `boundCache.Cache()`; `Runtime()` answers the runtime the view is bound to.

## Usage

The example below demonstrates a typical Melody flow:

1. Override the cache backend and cache service **before boot** (application wiring).
2. Bind the cache to the runtime.
3. Use `Remember` for cache-aside reads with stampede protection.

`Remember` answers the type its callback names, read back through the serializer into that type (see [`cache/json_serializer.go`](../../cache/json_serializer.go)); `Remember[any]` answers the encoding's generic form.

```go
package main

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

const userProfileCacheKey = "example.userProfile:42"

type userProfile struct {
    Id   int64
    Name string
}

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
                resolver.Runtime().Context(),
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
                resolver.Runtime().Context(),
                resolver,
            )
            serializerInstance := cache.CacheSerializerMustFromResolver(
                resolver.Runtime().Context(),
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
    runtimeInstance containercontract.Runtime,
) (userProfile, error) {
    return cache.Remember(
        cache.FromRuntime(runtimeInstance),
        userProfileCacheKey,
        cache.RememberConfig{
            Ttl: bound.Of(30 * time.Minute),
        },
        func(flightRuntime containercontract.Runtime, _ *cache.RememberItem) (userProfile, error) {
            _ = flightRuntime

            return userProfile{
                Id:   42,
                Name: "demo",
            }, nil
        },
    )
}
```

### The callback's item

The callback receives a `*cache.RememberItem` beside its runtime, through which it says what the store does with the value it computes: nothing said stores it under `RememberConfig.Ttl`, `item.SetTtl(ttl)` stores it under its own ttl, and `item.SkipStore()` answers it without storing it. An absence remembered for a short while and a found value kept longer is one callback:

```go
func findUser(
    boundCache cache.Bound,
    key string,
    load func(flightRuntime containercontract.Runtime) (*user, error),
) (*user, error) {
    return cache.Remember(
        boundCache,
        key,
        cache.RememberConfig{
            Ttl: bound.Of(time.Minute),
        },
        func(flightRuntime containercontract.Runtime, item *cache.RememberItem) (*user, error) {
            loaded, loadErr := load(flightRuntime)
            if nil != loadErr {
                return nil, loadErr
            }

            if nil != loaded {
                item.SetTtl(bound.Of(10 * time.Minute))
            }

            return loaded, nil
        },
    )
}
```

A value that must not be served from the cache, a partial answer of a degraded upstream for one, is answered and not stored:

```go
func(flightRuntime containercontract.Runtime, item *cache.RememberItem) (report, error) {
    computed, isPartial, computeErr := buildReport(flightRuntime)
    if nil != computeErr {
        return report{}, computeErr
    }

    if true == isPartial {
        item.SkipStore()
    }

    return computed, nil
}
```

### The flight's runtime

Under the stampede protection the callers of one key coalesce under a leader, and the computation belongs to the flight, not to any of them. The callback receives the flight's runtime, never a caller's: a run of its own, `KindRun` whatever the leader's kind, over the flight's context, in a scope the flight opens behind the leader's and closes once it completed. A callback resolves its services through it whichever request ends first, and a service it builds in that scope is closed with the flight; a name reserved for the request phase is refused inside a callback for every caller alike. A leader whose runtime opens no child run, the boot runtime, a provider's runtime or a scope of another implementation, lends its own scope over the flight's context: the computation runs on, and a resolution through that scope is refused once the boot finished or the provider returned. A leader whose scope is closed, a view kept past its request, is answered the closed scope's error, nothing computed or stored. With no flight, the stampede protection disabled or a cache the flight cannot tell apart by its pointer, the callback runs under the caller's runtime. Opening and closing the flight's scope costs about 27 allocations per led miss, not per caller; the read before the flight and every store run under a runtime derived without the cancellation of the caller's context, one allocation on every `Remember`.

## Footguns & caveats

- A read names its type through its target, a non-nil pointer, or through `cache.Get[T]`: a hit of `Get` overwrites the target whole, and a miss, a failure or a payload that does not decode leaves it as it was; `Many` replaces the map with the entries that decoded once the store answered. A target that is nil or no pointer is refused before the store is read, by an error that is no `DeserializationError`. One key holds one type. Which types refuse each other is the serializer's: json refuses a mismatch of kind only, so a string read as a struct is a `DeserializationError`, a miss to `Remember` that recomputes and overwrites it, while an object written as one struct reads as another struct with the fields they do not share at zero, a hit; give each type its own key. The json serializer reads a field the payload lacks as its zero value and ignores a key the type lacks, so a cached shape that changes between releases carries a version inside the value or moves the key.
- `Manager.Get` returns `exists == false` when deserialization fails, and the error is a typed [`DeserializationError`](../../cache/deserialization_error.go) naming the key. `Remember` treats that type as a miss: the callback recomputes and overwrites the corrupt payload, so the key heals; every other error keeps meaning the cache itself failed. A `Cache` implementation of your own inherits the self-healing by wrapping its deserialization failures with `cache.NewDeserializationError`.
- `Manager.Many` leaves a corrupt entry out of the result the way an absent key is, and returns the good values **beside** a `DeserializationError` naming the corrupt keys in requested order — check the error even when the map is non-empty.
- Values round-trip through the serializer, and under `Remember[any]` or `cache.Get[any]` the default `NewJsonSerializer` decodes every JSON number into a `float64`: an integer beyond 2^53 comes back a **different integer**, with no decoding error to notice — the same hazard [`SESSION.md`](SESSION.md) records for its file storage. `Remember` runs the round-trip on its own computing call too, so the miss answers the same shape every hit does, which means the loss is uniform rather than only on the second call: a callback that returns `int64(1<<60)` reads back changed on the cold path as well. The `int64(42)` above is safe because it is under 2^53; a snowflake id, a `bigint` key, a minor-unit amount or a `UnixNano()` is not. Carry a version or a string inside the value, or key such a value so it never decodes through JSON. A read typed as an `int64` keeps the integer whole. The round-trip runs BEFORE the store: a value the serializer encodes but cannot decode — the JSON serializer has no nesting ceiling on the way in and one of ten thousand on the way out — is refused without being written, so the key stays empty and the caller gets the refusal once per call; stored first, it was read back as a typed miss on every later call, recomputed, rewritten and refused again, a loop no ttl ended.
- `NewManager` builds a manager that does **not** own its backend: `CloseWithContext` leaves the backend open, which is what the container path needs — both are registered services and the container closes each one itself. `ManagerConfig{OwnsBackend: true}` keeps the cascade for the caller that builds both by hand.
- A closed `InMemoryBackend` refuses every operation with `cache backend is closed`; `CloseWithContext` stays idempotent, and past its deadline answers the deadline's error while the cleanup loop leaves on its own. The key grammar is part of the [`Backend`](../../cache/contract/backend.go) contract: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and a malformed key is refused on every operation by both this backend and the `rueidis` backend, with the same message and in the same order, at the same limit unless the `rueidis` backend's `BackendConfig.MaxKeyLength` moves its own, and both echo the key cut to 128 bytes.
- `Remember` coalesces concurrent callers per **cache instance, key, value type and cancelability** — the instance is told apart by its pointer. Two managers over one backend are two coalescing units, and a value-kind `Cache` implementation gets no coalescing at all (every caller runs its own callback); implement `Cache` on a pointer receiver to coalesce.
- Every caller of one flight owns its answer: the first takes the value the flight produced and every later one a copy made through the cache's `RoundTrip`, a `Manager`'s serializer round trip, so no two callers share a map or a slice, whatever decorator embeds the cache. A stored value the leader cannot copy is read again from the store by every caller after the first, as a hit outside a flight reads it, and a caller that finds the entry gone by then, or no longer decoding, computes and stores as a miss outside a flight does; a computed value it cannot copy is refused. A `Cache` of your own that holds values rather than payloads answers in `RoundTrip` a copy as deep as its reads are; one that assigns hands the callers of one flight one value by its own declaration.
- The zero `RememberConfig` but for its required `Ttl` is the default: stampede protection on, a 30s wait, a **cancelable** flight. A `WaitTimeout` of `bound.Of(time.Duration(0))` means "no waiting": a result already memoized by the in-flight call is returned, anything still computing answers with the timeout error; `bound.UnlimitedDuration()` waits on the flight and the context alone.
- Under the **cancelable** default the flight is canceled when its last waiter leaves, the next caller of the key replacing the canceled flight with a fresh leader, so a hung callback does not own its key. `Detached: true` is the **detached** flight: a leader whose waiters have all left keeps running with its final error written into an in-flight record nothing reads anymore, and a callback that never returns pins its single-flight entry for the life of the process: a later detached caller of the key joins it and waits until its own `WaitTimeout` or context ends, never past them, and a cancelable caller does not join it at all. Give long callbacks their own deadlines either way. The two settings compose one way to know: under the cancelable default a `WaitTimeout` **shorter than the callback** never stores — the lone waiter times out, the flight loses its last waiter and is canceled, the callback that honours its context returns the cancellation, and the next call leads a fresh flight to the same end, so the key is never populated. A bounded wait on a slow callback is paired with `Detached: true`, which lets the
  computation finish and store for the callers after.
- The runtime of the view, `Remember`'s first parameter, ties the **wait** to the request that opened it through its context, and nothing else; a nil runtime, a typed nil and one carrying no context or no scope are refused. A caller that serves an http request binds the handler's `runtimeInstance`, which the kernel builds on the request's context and which the example's handlers hand their services' read doors, so a client that disconnected releases the goroutine that served it, with its scope, its session and everything else the request reached, before the 30s wait ends. The context ends that one caller's wait with `cache remember wait canceled by the caller context`, carrying `context.Canceled` or `context.DeadlineExceeded` as the cause; it never ends the computation, which belongs to the leader and is awaited by everyone coalesced onto it. What cancels the computation is still the last waiter leaving, and only on the cancelable flight, the default. An answer already memoized when the wait begins is handed over whatever the context says. Where the **callback** is what should stop, give it its own deadline: the runtime it receives
  is the flight's, not the caller's — except on the uncoalesced path (`StampedeProtectionDisabled`, or a value-kind `Cache`), where there is no flight and the callback runs under the caller's runtime.
- Every operation of a `Cache` takes the runtime it runs under first, a nil runtime, a typed nil and one carrying no context refused with `the runtime is nil or carries no context` before anything else is judged; the manager hands its backend the same runtime. Every operation of a `Backend` takes the runtime first too, an absent one refused with the same error before anything else is judged, the closed backend included; a runtime whose context is already done answers that context's error once the arguments are judged, before the store is touched, so a refused key still answers the key's refusal. The `rueidis` backend bounds each round trip by the earlier of the runtime's context's deadline and its `BackendConfig.CallTimeout`, and a `Clear` walk by the context as a whole; a round trip its call timeout cut names the field, with its `ErrCallTimeout` in the chain, while one the context's own deadline cut answers the context's error, wrapped on `Increment` and `Decrement` under `cache counter operation failed` (`errors.Is` holds).
- `Remember` reads the cache and stores a computed value under a runtime derived without the cancellation of the context each runs under, the caller's or the flight's, its values and its scope kept: a done context still answers a hit, and a value already computed is stored whoever left. The derived runtime is one allocation on every `Remember`, a hit included.
- `Manager.Increment` and `Manager.Decrement` are backend-native, so a distributed backend keeps them atomic and the count is stored as decimal text rather than through the serializer. Each takes a [`CounterOptions`](../../cache/contract/counter.go): `Ttl` is the counter's lifetime, read as a write's (at least 1s, `bound.UnlimitedDuration()` for none, absent refused); `RefreshTtl` false writes it only when the call creates the key or finds it without an expiry, a fixed window, and true rewrites it on every call, a sliding window; an unlimited ttl writes no expiry, leaving one the key carries in place with `RefreshTtl` false and removing it with `RefreshTtl` true, so the counter is kept until it is deleted or evicted. The redis backend removes it with `PERSIST`. The redis backend writes the count and its expiry in one script, atomic under concurrent increments, with no `PEXPIRE … NX`, so it needs no Redis 7, but it needs scripting (`EVALSHA`/`EVAL`) allowed for the cache's redis user, with the commands the script runs under that user's permissions (`INCRBY`, `PTTL`, `PEXPIRE`, `PERSIST`, `GET`); after a script cache flush the first call makes two round trips under one `CallTimeout`. A counter that expired reads as absent and the next increment starts from its delta. Read such a key with `GetCounter`, the contract's door that counts nothing and answers an absent or expired key as false with no error, through the view as `boundCache.GetCounter(key)`, never with `Get`, and do not mix `Set` onto the same key. See [`cache/manager.go`](../../cache/manager.go).
- `Remember` uses a single-flight mechanism when stampede protection is enabled (default). See [`cache/remember.go`](../../cache/remember.go).
- The callback's item is read once, when the callback returns without an error: a callback that fails or panics stores nothing whatever its item says, and an item kept and told something later changes no store. A ttl chosen through `SetTtl` is any ttl the cache admits, unlimited included, longer than the configuration's too, and one below a second is answered as the error naming `RememberItem.Ttl`, with nothing stored and no value answered; an absent one takes the choice back. `SkipStore` wins over `SetTtl` in either order: the caller and every waiter of the flight are answered the value, each a copy through `RoundTrip`, and the next call computes again, so under a skipped store coalescing is the only stampede protection left. `RememberConfig.Ttl` stays required even where every callback chooses, since it is judged before the cache is read. See [`cache/remember_item.go`](../../cache/remember_item.go).
- `Remember` groups in-flight calls by cache instance, key, and cancelability (cancelable callers are isolated from non-cancelable callers). See [`cache/remember.go`](../../cache/remember.go).
- A cancelable in-flight call whose waiters have all timed out is abandoned: a caller that joins afterwards does not inherit the cancellation error — it starts a fresh computation. See [`cache/remember.go`](../../cache/remember.go).
- [`InMemoryBackend`](../../cache/in_memory.go) owns a cleanup goroutine whose lifetime ends when [`CloseWithContext`](../../cache/in_memory.go) is called; callers must close the backend — or a `Manager` built over it with `ManagerConfig{OwnsBackend: true}` — to stop it; there is no finalizer fallback, and a manager that does not own its backend does not do it for you.

## Userland API

### Contracts (`cache/contract`)

#### Types

- **Cache** ([`cache/contract/cache.go`](../../cache/contract/cache.go))

```go
package main

type Cache interface {
    Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error)
    Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration) error
    Delete(runtimeInstance containercontract.Runtime, key string) error
    Has(runtimeInstance containercontract.Runtime, key string) (bool, error)
    Clear(runtimeInstance containercontract.Runtime) error

    Many(runtimeInstance containercontract.Runtime, keys []string, target any) error
    SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration) error
    DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error

    Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)
    Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)
    GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error)

    RoundTrip(value any, target any) error

    CloseWithContext(closeContext context.Context) error
}
```

- **Backend** ([`cache/contract/backend.go`](../../cache/contract/backend.go))
- **TtlRule()** and **ResolveTtl** ([`cache/contract/ttl.go`](../../cache/contract/ttl.go)) — the reading every backend gives the ttl of a write: absent refused, at least 1s, unlimited answered as 0, no expiry
- **Serializer** ([`cache/contract/serializer.go`](../../cache/contract/serializer.go)) — `Serialize(value) ([]byte, error)` and `Deserialize(payload, target) error`, decoding into the target, the signature of [`serializer/contract.Serializer`](../../serializer/contract/serializer.go), so `serializer.NewJsonSerializer()` serves as a cache serializer

### Types

- **cache.Bound** ([`cache/bound.go`](../../cache/bound.go)) — the cache bound to one runtime: `Get`, `Set`, `Delete`, `Has`, `Clear`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`, `GetCounter` without the runtime, `Runtime()` and `Cache()`; a zero view or one over no cache is refused by its first door
- **cache.Manager** ([`cache/manager.go`](../../cache/manager.go)) — every door refuses a nil runtime, a typed nil and one carrying no context before it reads anything; `GetCounter(runtimeInstance, key)` reads a counter
    - [`RoundTrip(value, target any) error`](../../cache/manager.go) — what a value stored through this manager reads back as, written into the target: one serializer round trip, run locally with no backend involved. `Remember` gives through it every caller of one flight a value of its own, the computing call the shape every hit answers, and refuses once a value the serializer writes but cannot read back
- **cache.InMemoryBackend** ([`cache/in_memory.go`](../../cache/in_memory.go))
- **cache.JsonSerializer** ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- **cache.InMemoryBackendConfig** ([`cache/in_memory.go`](../../cache/in_memory.go)) — `MaxItems`, `CleanupInterval` and `Clock`, the zero value the default
- **cache.ManagerConfig** ([`cache/manager.go`](../../cache/manager.go)) — `OwnsBackend`, off by default
- **cache.RememberConfig** ([`cache/remember.go`](../../cache/remember.go)) — `Ttl` required, `StampedeProtectionDisabled`, `WaitTimeout` (30s, 0 no wait, unlimited) and `Detached`
- **cache.RememberItem** ([`cache/remember_item.go`](../../cache/remember_item.go)) — `SetTtl(ttl)`, `SkipStore()`, `Ttl()` and `IsStoreSkipped()`; the zero value and a nil item say nothing
- **cache.DeserializationError** ([`cache/deserialization_error.go`](../../cache/deserialization_error.go)) — the typed error a read answers when the payload does not deserialize; `Remember` treats it as a miss.
- **cache.Item** ([`cache/item.go`](../../cache/item.go)) — the entry representation `InMemoryBackend` keeps internally. Carries key, payload, creation/expiration timestamps, last-access time, and hit count; `cachecontract.Backend.Get` itself returns the raw payload bytes, not an `Item`.

### Constructors

- `cache.For(runtimeInstance, cacheInstance) cache.Bound` ([`cache/bound.go`](../../cache/bound.go)) — allocates nothing and never panics
- `cache.FromRuntime(runtimeInstance) cache.Bound` ([`cache/bound.go`](../../cache/bound.go)) — resolves `cache.ServiceCache` through the runtime's scope and panics where it is missing
- `cache.NewManager(backend, serializer, config) *cache.Manager` ([`cache/manager.go`](../../cache/manager.go)) — the zero config does not own the backend, which the container path wants; `OwnsBackend` cascades Close into the backend, for the caller that builds both by hand.
- `cache.NewInMemoryBackend(config) *cache.InMemoryBackend` ([`cache/in_memory.go`](../../cache/in_memory.go))
- `cache.NewDeserializationError(keys, causeErr) *cache.DeserializationError` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
- `cache.NewJsonSerializer() cachecontract.Serializer` ([`cache/json_serializer.go`](../../cache/json_serializer.go))
- `cache.NewItem(key string, payload []byte, config cache.ItemConfig) *cache.Item` ([`cache/item.go`](../../cache/item.go)) — `CreatedAt` and `ExpiresAt`, nil for an item that does not expire, for a `Backend` of the application's own

### Retrieval helpers

- `cache.CacheMustFromResolver(ctx, resolver) cachecontract.Cache`
- `cache.CacheBackendMustFromResolver(ctx, resolver) cachecontract.Backend`
- `cache.CacheSerializerMustFromResolver(ctx, resolver) cachecontract.Serializer`

### Functions

#### Cache-aside

- `cache.Remember[T](boundCache, key, config, callback) (T, error)` ([`cache/remember.go`](../../cache/remember.go)) — `T` inferred from the callback, `func(runtimeInstance containercontract.Runtime, item *cache.RememberItem) (T, error)`, handed the flight's runtime

#### Typed reads

- `cache.Get[T](boundCache, key) (T, bool, error)` ([`cache/typed.go`](../../cache/typed.go)) — the zero `T` beside any error
- `cache.Many[T](boundCache, keys) (map[string]T, error)` ([`cache/typed.go`](../../cache/typed.go)) — an empty map on no hit, the undecodable keys in a `DeserializationError` beside the entries that decoded

#### Errors

- `cache.IsDeserializationError(err) bool` ([`cache/deserialization_error.go`](../../cache/deserialization_error.go))
