package contract

import "time"

/* Backend is one promise with several implementations, so the key grammar is part of it: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and every implementation refuses a malformed key with the same answer. The refusal order is shared too: a closed backend answers before the key or the ttl is judged, and a batch write judges the ttl before its keys. A nil payload is stored as the empty payload and reads back as an empty non-nil slice. */
type Backend interface {
    Get(key string) ([]byte, bool, error)

    Set(key string, payload []byte, ttl time.Duration) error

    Delete(key string) error

    Has(key string) (bool, error)

    Clear() error

    Many(keys []string) (map[string][]byte, error)

    SetMultiple(items map[string][]byte, ttl time.Duration) error

    DeleteMultiple(keys []string) error

    Increment(key string, delta int64) (int64, error)

    Decrement(key string, delta int64) (int64, error)

    Close() error
}
