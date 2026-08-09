package contract

import "time"

/* Backend is one promise with more than one implementation, so the key grammar is part of the promise: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and every implementation refuses a malformed key with the same answer. Without the shared grammar a caller could tell the backends apart by which keys they accept — a key built from user input working against the development backend and failing against the production store on every operation. */
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
