package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Cache is the application's view of a store of typed values. A read names the type it wants through its target, a non-nil pointer: a *T is filled with a T, a *any with the generic form of the store's encoding. A target that is nil or no pointer is refused before the store is read, by an error that is no DeserializationError. Get answers the target whole or leaves it untouched: on a hit the target is overwritten, never merged into what it held; on a miss, a failure or a payload that does not decode it keeps what it held. A payload the serializer refuses to decode into the target's type answers cache.DeserializationError naming the key, which Remember reads as a miss; which types refuse each other is the serializer's: json refuses a mismatch of kind only, so an object written as one struct reads as another struct with the fields they do not share at zero. One key holds one type. Every door but RoundTrip runs under the runtime it is handed, refused when nil or carrying no context; userland calls a door through cache.Bound, bound once to its request, run or delivery. */
type Cache interface {
    Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error)

    Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration) error

    Delete(runtimeInstance containercontract.Runtime, key string) error

    Has(runtimeInstance containercontract.Runtime, key string) (bool, error)

    Clear(runtimeInstance containercontract.Runtime) error

    /* Many reads the keys into target, a non-nil pointer to a map[string]T, nil admitted behind the pointer: once the store answered, the map is replaced by one holding exactly the keys that are present and decoded. An entry that does not decode is left out as an absent key is, and its keys come back in one DeserializationError beside the entries that decoded. */
    Many(runtimeInstance containercontract.Runtime, keys []string, target any) error

    SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration) error

    DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error

    Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)

    Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)

    /* RoundTrip writes into target what value reads back as once stored: the cache's own encoding applied and undone locally, the store untouched. Remember gives through it every caller of one flight a value of its own, and refuses once a value that can be written but not read back as its type. A cache that stores values rather than payloads answers a copy as deep as its reads are. */
    RoundTrip(value any, target any) error

    CloseWithContext(closeContext context.Context) error
}
