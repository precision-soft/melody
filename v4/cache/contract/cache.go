package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
)

type Cache interface {
    Get(ctx context.Context, key string) (any, bool, error)

    Set(ctx context.Context, key string, value any, ttl bound.Duration) error

    Delete(ctx context.Context, key string) error

    Has(ctx context.Context, key string) (bool, error)

    Clear(ctx context.Context) error

    Many(ctx context.Context, keys []string) (map[string]any, error)

    SetMultiple(ctx context.Context, items map[string]any, ttl bound.Duration) error

    DeleteMultiple(ctx context.Context, keys []string) error

    Increment(ctx context.Context, key string, delta int64) (int64, error)

    Decrement(ctx context.Context, key string, delta int64) (int64, error)

    CloseWithContext(closeContext context.Context) error
}
