package cache

import (
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

func NewManager(
    backend cachecontract.Backend,
    serializer cachecontract.Serializer,
) *Manager {
    if true == internal.IsNilInterface(backend) {
        exception.Panic(exception.NewError("cache backend is nil", nil, nil))
    }

    if true == internal.IsNilInterface(serializer) {
        exception.Panic(exception.NewError("cache serializer is nil", nil, nil))
    }

    return &Manager{
        backend:    backend,
        serializer: serializer,
    }
}

type Manager struct {
    backend    cachecontract.Backend
    serializer cachecontract.Serializer
}

func (instance *Manager) Get(key string) (any, bool, error) {
    payload, exists, getErr := instance.backend.Get(key)
    if nil != getErr {
        return nil, false, getErr
    }

    if false == exists {
        return nil, false, nil
    }

    value, deserializeErr := instance.serializer.Deserialize(payload)
    if nil != deserializeErr {
        return nil, false, deserializeErr
    }

    return value, true, nil
}

func (instance *Manager) Set(key string, value any, ttl time.Duration) error {
    payload, serializeErr := instance.serializer.Serialize(value)
    if nil != serializeErr {
        return serializeErr
    }

    return instance.backend.Set(key, payload, ttl)
}

func (instance *Manager) Delete(key string) error {
    return instance.backend.Delete(key)
}

func (instance *Manager) Has(key string) (bool, error) {
    return instance.backend.Has(key)
}

func (instance *Manager) Clear() error {
    return instance.backend.Clear()
}

func (instance *Manager) Many(keys []string) (map[string]any, error) {
    payloadsByKey, manyErr := instance.backend.Many(keys)
    if nil != manyErr {
        return nil, manyErr
    }

    result := make(map[string]any, len(payloadsByKey))
    for key, payload := range payloadsByKey {
        value, deserializeErr := instance.serializer.Deserialize(payload)
        if nil != deserializeErr {
            return nil, deserializeErr
        }

        result[key] = value
    }

    return result, nil
}

func (instance *Manager) SetMultiple(items map[string]any, ttl time.Duration) error {
    payloads := make(map[string][]byte, len(items))
    for key, value := range items {
        payload, serializeErr := instance.serializer.Serialize(value)
        if nil != serializeErr {
            return serializeErr
        }

        payloads[key] = payload
    }

    return instance.backend.SetMultiple(payloads, ttl)
}

func (instance *Manager) DeleteMultiple(keys []string) error {
    return instance.backend.DeleteMultiple(keys)
}

func (instance *Manager) Increment(key string, delta int64) (int64, error) {
    return instance.backend.Increment(key, delta)
}

func (instance *Manager) Decrement(key string, delta int64) (int64, error) {
    return instance.backend.Decrement(key, delta)
}

func (instance *Manager) Close() error {
    return instance.backend.Close()
}

var _ cachecontract.Cache = (*Manager)(nil)
