package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
)

/* Storage keeps sessions and is safe for concurrent use: the manager serialises Save and Delete per id only, and Load runs unlocked. Each operation refuses, in order, a nil context (RefuseNilContext), a closed storage, a bad id or ttl (ResolveTtl), then a done context, before the store is touched. */
type Storage interface {
    Load(ctx context.Context, sessionId string) (map[string]any, bool, error)

    Save(ctx context.Context, sessionId string, data map[string]any, ttl bound.Duration) error

    Delete(ctx context.Context, sessionId string) error

    CloseWithContext(closeContext context.Context) error
}
