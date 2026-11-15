package cache

import (
    "context"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/redis/rueidis"
)

func NewBackendService(client rueidis.Client, config BackendConfig) (*BackendService, error) {
    backend, err := NewBackend(client, config)
    if nil != err {
        return nil, err
    }

    return &BackendService{
        backend: backend,
    }, nil
}

/* BackendService holds the cache backend built over a redis client, fixed at construction. The entries live in redis, so one service serves concurrent callers without a lock; the one state it changes is the backend's closed flag, an atomic that CloseWithContext sets. */
type BackendService struct {
    backend *Backend
}

func (instance *BackendService) Backend() *Backend {
    return instance.backend
}

func (instance *BackendService) Get(ctx context.Context, key string) ([]byte, bool, error) {
    return instance.backend.Get(ctx, key)
}

func (instance *BackendService) Set(ctx context.Context, key string, payload []byte, ttl bound.Duration) error {
    return instance.backend.Set(ctx, key, payload, ttl)
}

func (instance *BackendService) Delete(ctx context.Context, key string) error {
    return instance.backend.Delete(ctx, key)
}

func (instance *BackendService) Has(ctx context.Context, key string) (bool, error) {
    return instance.backend.Has(ctx, key)
}

func (instance *BackendService) Clear(ctx context.Context) error {
    return instance.backend.Clear(ctx)
}

func (instance *BackendService) ClearByPrefix(ctx context.Context, prefix string) error {
    return instance.backend.ClearByPrefix(ctx, prefix)
}

func (instance *BackendService) Many(ctx context.Context, keys []string) (map[string][]byte, error) {
    return instance.backend.Many(ctx, keys)
}

func (instance *BackendService) SetMultiple(ctx context.Context, items map[string][]byte, ttl bound.Duration) error {
    return instance.backend.SetMultiple(ctx, items, ttl)
}

func (instance *BackendService) DeleteMultiple(ctx context.Context, keys []string) error {
    return instance.backend.DeleteMultiple(ctx, keys)
}

func (instance *BackendService) Increment(ctx context.Context, key string, delta int64) (int64, error) {
    return instance.backend.Increment(ctx, key, delta)
}

func (instance *BackendService) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
    return instance.backend.Decrement(ctx, key, delta)
}

func (instance *BackendService) CloseWithContext(closeContext context.Context) error {
    return instance.backend.CloseWithContext(closeContext)
}

var _ cachecontract.Backend = (*BackendService)(nil)

/* BackendFromRuntime panics when the service is absent, despite carrying no Must in its name: it wraps the framework's MustFromRuntime and has no error slot. */
func BackendFromRuntime(runtimeInstance runtimecontract.Runtime, serviceName string) *Backend {
    return runtime.MustFromRuntime[*BackendService](runtimeInstance, serviceName).Backend()
}
