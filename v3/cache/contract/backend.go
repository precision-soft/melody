package contract

import "time"

/* Backend keys are non-empty, contain neither ASCII spaces nor newlines, and are at most 1024 bytes. Implementations apply the same refusal order: closed state first, then batch TTL, then keys. Nil payloads are stored and returned as non-nil empty byte slices. */
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
