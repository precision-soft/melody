package lock

import (
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type lazyLocker struct {
    locker *container.LazyService[lockcontract.Locker]
}

/* NewLazyLocker returns a Locker that resolves the registered service.lock.locker on first use; a successful resolution is reused for every later call. A failed resolution never panics: CreateLock returns a lock whose every method reports the resolution error, so LeaderGate and RunExclusive see a store outage as the acquire error they already handle, and the resolution is retried on the next CreateLock. */
func NewLazyLocker(resolver containercontract.Resolver) lockcontract.Locker {
    return &lazyLocker{
        locker: container.Lazy[lockcontract.Locker](resolver, ServiceLocker),
    }
}

func (instance *lazyLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    locker, resolveErr := instance.locker.Resolve()
    if nil != resolveErr {
        return &unresolvedLock{resolveErr: resolveErr}
    }

    if true == internal.IsNilInterface(locker) {
        return &unresolvedLock{
            resolveErr: exception.NewError(
                "lazy locker resolved to nil",
                exceptioncontract.Context{
                    "serviceName": ServiceLocker,
                },
                nil,
            ),
        }
    }

    return locker.CreateLock(name, ttl)
}

type unresolvedLock struct {
    resolveErr error
}

func (instance *unresolvedLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return false, instance.resolveErr
}

func (instance *unresolvedLock) Release(runtimeInstance runtimecontract.Runtime) error {
    return instance.resolveErr
}

func (instance *unresolvedLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    return instance.resolveErr
}

var (
    _ lockcontract.Locker = (*lazyLocker)(nil)
    _ lockcontract.Lock   = (*unresolvedLock)(nil)
)
