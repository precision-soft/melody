package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
)

/* Backend is one promise with several implementations, so the key grammar is part of it: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and every implementation refuses a malformed key with the same answer. The refusal order is shared too: a closed backend answers before the key or the ttl is judged, and a batch write judges the ttl before its keys. The ttl is read through ResolveTtl, so every implementation refuses the same ttls with the same answer. A nil payload is stored as the empty payload and reads back as an empty non-nil slice. Every operation takes the caller's context first: a nil context is refused before anything else is judged, and a context already done answers its error once the arguments are judged, before the store is touched; a store reached over the network bounds each round trip by the earlier of the context's deadline and its own command timeout. */
type Backend interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)

    Set(ctx context.Context, key string, payload []byte, ttl bound.Duration) error

    Delete(ctx context.Context, key string) error

    Has(ctx context.Context, key string) (bool, error)

    Clear(ctx context.Context) error

    Many(ctx context.Context, keys []string) (map[string][]byte, error)

    SetMultiple(ctx context.Context, items map[string][]byte, ttl bound.Duration) error

    DeleteMultiple(ctx context.Context, keys []string) error

    Increment(ctx context.Context, key string, delta int64) (int64, error)

    Decrement(ctx context.Context, key string, delta int64) (int64, error)

    CloseWithContext(closeContext context.Context) error
}
