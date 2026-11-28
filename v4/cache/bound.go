package cache

import (
    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

/* Bound is a cache bound to one runtime: every door runs under it. It is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field: a service holds the cache and binds it at the top of each method. A call that needs another deadline binds again over runtime.WithContext. */
type Bound struct {
    runtimeInstance containercontract.Runtime
    cacheInstance   cachecontract.Cache
}

/* For binds the cache to the runtime; it allocates nothing and never panics, a nil of either being refused by the first door called. */
func For(runtimeInstance containercontract.Runtime, cacheInstance cachecontract.Cache) Bound {
    return Bound{
        runtimeInstance: runtimeInstance,
        cacheInstance:   cacheInstance,
    }
}

/* FromRuntime resolves the application's cache through the runtime's scope and binds it; a missing cache panics as runtime.MustFromRuntime does. The resolution is the cost, so a function binds once and calls many times. */
func FromRuntime(runtimeInstance containercontract.Runtime) Bound {
    return For(runtimeInstance, runtime.MustFromRuntime[cachecontract.Cache](runtimeInstance, ServiceCache))
}

/* Runtime answers the runtime the view is bound to, for a callee that binds something else to it. */
func (instance Bound) Runtime() containercontract.Runtime {
    return instance.runtimeInstance
}

/* Cache answers the unbound cache, for RoundTrip and for a callee that binds it to another runtime. */
func (instance Bound) Cache() cachecontract.Cache {
    return instance.cacheInstance
}

func (instance Bound) Get(key string, target any) (bool, error) {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return false, nilErr
    }

    return instance.cacheInstance.Get(instance.runtimeInstance, key, target)
}

func (instance Bound) Set(key string, value any, ttl bound.Duration, options cachecontract.SetOptions) error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.Set(instance.runtimeInstance, key, value, ttl, options)
}

func (instance Bound) Delete(key string) error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.Delete(instance.runtimeInstance, key)
}

func (instance Bound) Has(key string) (bool, error) {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return false, nilErr
    }

    return instance.cacheInstance.Has(instance.runtimeInstance, key)
}

func (instance Bound) Clear() error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.Clear(instance.runtimeInstance)
}

func (instance Bound) Many(keys []string, target any) error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.Many(instance.runtimeInstance, keys, target)
}

func (instance Bound) SetMultiple(items map[string]any, ttl bound.Duration, options cachecontract.SetOptions) error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.SetMultiple(instance.runtimeInstance, items, ttl, options)
}

func (instance Bound) DeleteMultiple(keys []string) error {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return nilErr
    }

    return instance.cacheInstance.DeleteMultiple(instance.runtimeInstance, keys)
}

func (instance Bound) Increment(key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return 0, nilErr
    }

    return instance.cacheInstance.Increment(instance.runtimeInstance, key, delta, options)
}

func (instance Bound) Decrement(key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return 0, nilErr
    }

    return instance.cacheInstance.Decrement(instance.runtimeInstance, key, delta, options)
}

func (instance Bound) GetCounter(key string) (int64, bool, error) {
    if nilErr := instance.refuseNilCache(); nil != nilErr {
        return 0, false, nilErr
    }

    return instance.cacheInstance.GetCounter(instance.runtimeInstance, key)
}

/* a typed-nil Cache passes a plain comparison and panics on its first method call */
func (instance Bound) refuseNilCache() error {
    if true == internal.IsNilInterface(instance.cacheInstance) {
        return exception.NewError("cache instance is nil", nil, nil)
    }

    return nil
}
