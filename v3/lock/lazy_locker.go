package lock

import (
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

/* lazyLocker defers resolving the registered Locker until the first CreateLock call, so a cli command or an http middleware assembled during boot can hold a Locker before the container is safe to resolve — the deferred-resolution proxy every consumer would otherwise hand-roll, shipped once over the framework's own contract. */
type lazyLocker struct {
    locker *container.LazyService[lockcontract.Locker]
}

/* NewLazyLocker returns a Locker that resolves the registered service.lock.locker on first use; the underlying Locker is resolved once and reused. */
func NewLazyLocker(resolver containercontract.Resolver) lockcontract.Locker {
    return &lazyLocker{
        locker: container.Lazy[lockcontract.Locker](resolver, ServiceLocker),
    }
}

func (instance *lazyLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    return instance.locker.Get().CreateLock(name, ttl)
}

var _ lockcontract.Locker = (*lazyLocker)(nil)
