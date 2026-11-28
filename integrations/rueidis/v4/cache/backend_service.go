package cache

import (
    "context"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

func NewBackendService(connection *melodyrueidis.Connection, config BackendConfig) (*BackendService, error) {
    backend, err := NewBackend(connection, config)
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

func (instance *BackendService) Get(runtimeInstance containercontract.Runtime, key string) ([]byte, bool, error) {
    return instance.backend.Get(runtimeInstance, key)
}

func (instance *BackendService) Set(runtimeInstance containercontract.Runtime, key string, payload []byte, ttl bound.Duration, options cachecontract.SetOptions) error {
    return instance.backend.Set(runtimeInstance, key, payload, ttl, options)
}

func (instance *BackendService) Delete(runtimeInstance containercontract.Runtime, key string) error {
    return instance.backend.Delete(runtimeInstance, key)
}

func (instance *BackendService) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    return instance.backend.Has(runtimeInstance, key)
}

func (instance *BackendService) Clear(runtimeInstance containercontract.Runtime) error {
    return instance.backend.Clear(runtimeInstance)
}

func (instance *BackendService) ClearByPrefix(runtimeInstance containercontract.Runtime, prefix string) error {
    return instance.backend.ClearByPrefix(runtimeInstance, prefix)
}

func (instance *BackendService) Many(runtimeInstance containercontract.Runtime, keys []string) (map[string][]byte, error) {
    return instance.backend.Many(runtimeInstance, keys)
}

func (instance *BackendService) SetMultiple(runtimeInstance containercontract.Runtime, items map[string][]byte, ttl bound.Duration, options cachecontract.SetOptions) error {
    return instance.backend.SetMultiple(runtimeInstance, items, ttl, options)
}

func (instance *BackendService) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    return instance.backend.DeleteMultiple(runtimeInstance, keys)
}

func (instance *BackendService) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.backend.Increment(runtimeInstance, key, delta, options)
}

func (instance *BackendService) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.backend.Decrement(runtimeInstance, key, delta, options)
}

func (instance *BackendService) CloseWithContext(closeContext context.Context) error {
    return instance.backend.CloseWithContext(closeContext)
}

var _ cachecontract.Backend = (*BackendService)(nil)

/* BackendFromRuntime panics when the service is absent, despite carrying no Must in its name: it wraps the framework's MustFromRuntime and has no error slot. */
func BackendFromRuntime(runtimeInstance containercontract.Runtime, serviceName string) *Backend {
    return runtime.MustFromRuntime[*BackendService](runtimeInstance, serviceName).Backend()
}
