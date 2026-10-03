package cache

import (
    "context"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/redis/rueidis"
)

func NewBackendService(
    client rueidis.Client,
    prefix string,
    scanCount int,
    deleteBatch int,
) (*BackendService, error) {
    return NewBackendServiceWithCommandTimeout(client, prefix, scanCount, deleteBatch, 0)
}

/* NewBackendServiceWithCommandTimeout additionally bounds every contract call, whose cachecontract.Backend methods carry no context, so a request-path read against a store that stops answering does not hang the handler; a non-positive value reads as unbounded, the behaviour of NewBackendService. */
func NewBackendServiceWithCommandTimeout(
    client rueidis.Client,
    prefix string,
    scanCount int,
    deleteBatch int,
    commandTimeout time.Duration,
) (*BackendService, error) {
    backend, err := NewBackendWithCommandTimeout(
        client,
        context.Background(),
        prefix,
        scanCount,
        deleteBatch,
        commandTimeout,
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

/* WithContext binds a fresh handle to the given context over the same client and configuration. The handle shares the service's closed state, since the runtime door mints one per request and a handle ignoring Close would keep serving after its owner ended. */
func (instance *BackendService) WithContext(ctx context.Context) *Backend {
    if nil == ctx {
        return instance.backend
    }

    backend, err := NewBackendWithCommandTimeout(
        instance.client,
        ctx,
        instance.backend.prefix,
        instance.backend.scanCount,
        instance.backend.deleteBatch,
        instance.backend.commandTimeout,
    )

    if nil != err {
        exception.Panic(exception.FromError(err))
    }

    backend.ownerClosed = &instance.backend.closed

    return backend
}

func (instance *BackendService) Backend() *Backend {
    return instance.backend
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

/* BackendFromRuntime panics when the service is absent, despite carrying no Must in its name: it wraps the framework's MustFromRuntime and has no error slot. The name stays for compatibility. */
func BackendFromRuntime(runtimeInstance runtimecontract.Runtime, serviceName string) *Backend {
    return runtime.MustFromRuntime[*BackendService](runtimeInstance, serviceName).WithContext(runtimeInstance.Context())
}
