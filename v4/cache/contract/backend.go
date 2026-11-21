package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Backend is one promise with several implementations, so the key grammar is part of it: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and every implementation refuses a malformed key with the same answer. The refusal order is shared too: a closed backend answers before the key or the ttl is judged, and a batch write judges the ttl before its keys. The ttl is read through ResolveTtl, so every implementation refuses the same ttls with the same answer. A nil payload is stored as the empty payload and reads back as an empty non-nil slice. Every operation but CloseWithContext takes the runtime it runs under first: a nil runtime, a typed nil and one carrying no context are refused before anything else is judged, and a runtime whose context is already done answers that context's error once the arguments are judged, before the store is touched; a store reached over the network bounds each round trip by the earlier of the context's deadline and its own command timeout. */
type Backend interface {
    Get(runtimeInstance containercontract.Runtime, key string) ([]byte, bool, error)

    Set(runtimeInstance containercontract.Runtime, key string, payload []byte, ttl bound.Duration) error

    Delete(runtimeInstance containercontract.Runtime, key string) error

    Has(runtimeInstance containercontract.Runtime, key string) (bool, error)

    Clear(runtimeInstance containercontract.Runtime) error

    Many(runtimeInstance containercontract.Runtime, keys []string) (map[string][]byte, error)

    SetMultiple(runtimeInstance containercontract.Runtime, items map[string][]byte, ttl bound.Duration) error

    DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error

    Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)

    Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options CounterOptions) (int64, error)

    CloseWithContext(closeContext context.Context) error
}
