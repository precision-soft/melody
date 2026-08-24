package contract

import "time"

/* Backend is one promise with more than one implementation, so the key grammar is part of the promise: a key is non-empty, carries no spaces or newlines, and is at most 1024 bytes, and every implementation refuses a malformed key with the same answer. Without the shared grammar a caller could tell the backends apart by which keys they accept — a key built from user input working against the development backend and failing against the production store on every operation. The order of the refusals is shared too: a closed backend answers the closed refusal before the key or the ttl is judged, and a batch write judges the ttl before its keys, so a call that is wrong in more than one way is refused with the same answer whichever implementation it hit.

The payload identity is part of the promise for the same reason: a nil payload is stored as the empty payload and reads back as an empty non-nil slice — redis has no nil to store, so an implementation preserving the distinction would be tellable apart by reading back what was written. */
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
