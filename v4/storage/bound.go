package storage

import (
    "io"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* Bound is a storage bound to one runtime: every door runs under its context, and so does the read of an Object.Body a Get answered, after the door returned. It is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field: a service holds the storage and binds it at the top of each method. */
type Bound struct {
    runtimeInstance containercontract.Runtime
    storageInstance storagecontract.Storage
}

/* For binds the storage to the runtime; it allocates nothing and never panics, a nil of either being refused by the first door called. */
func For(runtimeInstance containercontract.Runtime, storageInstance storagecontract.Storage) Bound {
    return Bound{
        runtimeInstance: runtimeInstance,
        storageInstance: storageInstance,
    }
}

/* FromRuntime resolves the application's storage, ServiceStorage, through the runtime's scope and binds it; a missing one panics as runtime.MustFromRuntime does. */
func FromRuntime(runtimeInstance containercontract.Runtime) Bound {
    return For(runtimeInstance, runtime.MustFromRuntime[storagecontract.Storage](runtimeInstance, ServiceStorage))
}

/* Runtime answers the runtime the view is bound to, for a callee that binds something else to it. */
func (instance Bound) Runtime() containercontract.Runtime {
    return instance.runtimeInstance
}

/* Storage answers the unbound storage, for a callee that binds it to another runtime. */
func (instance Bound) Storage() storagecontract.Storage {
    return instance.storageInstance
}

func (instance Bound) Put(key string, reader io.Reader, size int64, options storagecontract.PutOptions) error {
    if nilErr := instance.refuseNilStorage(); nil != nilErr {
        return nilErr
    }

    return instance.storageInstance.Put(instance.runtimeInstance, key, reader, size, options)
}

func (instance Bound) Get(key string, options storagecontract.GetOptions) (storagecontract.Object, error) {
    if nilErr := instance.refuseNilStorage(); nil != nilErr {
        return storagecontract.Object{}, nilErr
    }

    return instance.storageInstance.Get(instance.runtimeInstance, key, options)
}

func (instance Bound) Stat(key string) (storagecontract.ObjectInfo, error) {
    if nilErr := instance.refuseNilStorage(); nil != nilErr {
        return storagecontract.ObjectInfo{}, nilErr
    }

    return instance.storageInstance.Stat(instance.runtimeInstance, key)
}

func (instance Bound) Delete(key string) error {
    if nilErr := instance.refuseNilStorage(); nil != nilErr {
        return nilErr
    }

    return instance.storageInstance.Delete(instance.runtimeInstance, key)
}

func (instance Bound) PresignedUrl(key string, config storagecontract.PresignConfig) (string, error) {
    if nilErr := instance.refuseNilStorage(); nil != nilErr {
        return "", nilErr
    }

    return instance.storageInstance.PresignedUrl(instance.runtimeInstance, key, config)
}

/* a typed-nil storage passes a plain comparison and panics on its first method call */
func (instance Bound) refuseNilStorage() error {
    if true == internal.IsNilInterface(instance.storageInstance) {
        return exception.NewError("storage instance is nil", nil, nil)
    }

    return nil
}
