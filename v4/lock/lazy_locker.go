package lock

import (
    "sync"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* lazyLocker defers resolving the registered Locker until a lock it handed out is first used, so a command or middleware assembled during boot can hold a Locker before the container is safe to resolve. */
type lazyLocker struct {
    locker *container.LazyService[lockcontract.Locker]
}

/* NewLazyLocker returns a Locker that resolves the registered service.lock.locker on first use, under the context of the runtime handed to the lock's first Acquire, Release or Refresh, so the wait on a locker another goroutine is building ends with that run; a successful resolution is reused for every later lock. CreateLock resolves nothing and never fails: a resolution failure is reported by the lock method that met it, so LeaderGate and RunExclusive see a store outage as the acquire error they already handle, and the resolution is retried on the next method call. */
func NewLazyLocker(resolver containercontract.Resolver) lockcontract.Locker {
    return &lazyLocker{
        locker: container.Lazy[lockcontract.Locker](resolver, ServiceLocker),
    }
}

func (instance *lazyLocker) CreateLock(name string, ttl bound.Duration) lockcontract.Lock {
    return &deferredLock{
        owner: instance,
        name:  name,
        ttl:   ttl,
    }
}

type deferredLock struct {
    owner *lazyLocker
    name  string
    ttl   bound.Duration
    mutex sync.Mutex
    lock  lockcontract.Lock
}

/* the Locker is resolved outside the mutex, so a wait on its creation never blocks the lock's other callers */
func (instance *deferredLock) resolvedLock(runtimeInstance runtimecontract.Runtime) (lockcontract.Lock, error) {
    instance.mutex.Lock()
    lock := instance.lock
    instance.mutex.Unlock()

    if nil != lock {
        return lock, nil
    }

    if true == internal.IsNilInterface(runtimeInstance) {
        return nil, exception.NewError(
            "runtime is required to resolve the lazy locker",
            exceptioncontract.Context{
                "serviceName": ServiceLocker,
            },
            nil,
        )
    }

    locker, resolveErr := instance.owner.locker.Resolve(runtimeInstance.Context())
    if nil != resolveErr {
        return nil, resolveErr
    }

    if true == internal.IsNilInterface(locker) {
        return nil, exception.NewError(
            "lazy locker resolved to nil",
            exceptioncontract.Context{
                "serviceName": ServiceLocker,
            },
            nil,
        )
    }

    createdLock := locker.CreateLock(instance.name, instance.ttl)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.lock {
        instance.lock = createdLock
    }

    return instance.lock, nil
}

func (instance *deferredLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    lock, resolveErr := instance.resolvedLock(runtimeInstance)
    if nil != resolveErr {
        return false, resolveErr
    }

    return lock.Acquire(runtimeInstance)
}

func (instance *deferredLock) Release(runtimeInstance runtimecontract.Runtime) error {
    lock, resolveErr := instance.resolvedLock(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return lock.Release(runtimeInstance)
}

func (instance *deferredLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl bound.Duration) error {
    lock, resolveErr := instance.resolvedLock(runtimeInstance)
    if nil != resolveErr {
        return resolveErr
    }

    return lock.Refresh(runtimeInstance, ttl)
}

var (
    _ lockcontract.Locker = (*lazyLocker)(nil)
    _ lockcontract.Lock   = (*deferredLock)(nil)
)
