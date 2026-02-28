package cache

import (
    "context"
    "time"

    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/redis/rueidis"
)

func NewBackendService(
    client rueidis.Client,
    prefix string,
    scanCount int,
    deleteBatch int,
) (*BackendService, error) {
    backend, err := NewBackend(
        client,
        context.Background(),
        prefix,
        scanCount,
        deleteBatch,
    )
    if nil != err {
        return nil, err
    }

    return &BackendService{
        client:  client,
        backend: backend,
    }, nil
}

type BackendService struct {
    client  rueidis.Client
    backend *Backend
}

func (instance *BackendService) WithContext(ctx context.Context) *Backend {
    if nil == ctx {
        return instance.backend
    }

    backend, err := NewBackend(
        instance.client,
        ctx,
        instance.backend.prefix,
        instance.backend.scanCount,
        instance.backend.deleteBatch,
    )

    if nil != err {
        exception.Panic(exception.FromError(err))
    }

    return backend
}

func (instance *BackendService) Get(key string) ([]byte, bool, error) {
    return instance.backend.Get(key)
}

func (instance *BackendService) Set(key string, payload []byte, ttl time.Duration) error {
    return instance.backend.Set(key, payload, ttl)
}

func (instance *BackendService) Delete(key string) error {
    return instance.backend.Delete(key)
}

func (instance *BackendService) Has(key string) (bool, error) {
    return instance.backend.Has(key)
}

func (instance *BackendService) Clear() error {
    return instance.backend.Clear()
}

func (instance *BackendService) ClearByPrefix(prefix string) error {
    return instance.backend.ClearByPrefix(prefix)
}

func (instance *BackendService) Many(keys []string) (map[string][]byte, error) {
    return instance.backend.Many(keys)
}

func (instance *BackendService) SetMultiple(items map[string][]byte, ttl time.Duration) error {
    return instance.backend.SetMultiple(items, ttl)
}

func (instance *BackendService) DeleteMultiple(keys []string) error {
    return instance.backend.DeleteMultiple(keys)
}

func (instance *BackendService) Increment(key string, delta int64) (int64, error) {
    return instance.backend.Increment(key, delta)
}

func (instance *BackendService) Decrement(key string, delta int64) (int64, error) {
    return instance.backend.Decrement(key, delta)
}

func (instance *BackendService) Close() error {
    return instance.backend.Close()
}

var _ cachecontract.Backend = (*BackendService)(nil)

func BackendFromRuntime(runtimeInstance runtimecontract.Runtime, serviceName string) *Backend {
    return runtime.MustFromRuntime[*BackendService](runtimeInstance, serviceName).WithContext(runtimeInstance.Context())
}
