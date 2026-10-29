package contract

import (
    "github.com/precision-soft/melody/v4/bound"
)

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
