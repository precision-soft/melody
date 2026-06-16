package lock

import (
    "sync"
    "sync/atomic"
    "time"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryLocker(clockInstance clockcontract.Clock) *InMemoryLocker {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(exception.NewError("lock clock is nil", nil, nil))
    }

    return &InMemoryLocker{
        clock:   clockInstance,
        holders: make(map[string]inMemoryHolder),
    }
}

const inMemoryPurgeInterval = 512

type InMemoryLocker struct {
    clock      clockcontract.Clock
    mutex      sync.Mutex
    holders    map[string]inMemoryHolder
    counter    uint64
    purgeTicks int
}

type inMemoryHolder struct {
    token     uint64
    expiresAt time.Time
}

func (instance *InMemoryLocker) CreateLock(name string, ttl time.Duration) lockcontract.Lock {
    token := atomic.AddUint64(&instance.counter, 1)

    return &inMemoryLock{
        locker: instance,
        name:   name,
        ttl:    ttl,
        token:  token,
    }
}

func (instance *InMemoryLocker) PurgeExpired() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    purged := 0
    for name, holder := range instance.holders {
        if false == instance.isActive(holder) {
            delete(instance.holders, name)
            purged++
        }
    }

    return purged
}

func (instance *InMemoryLocker) acquire(name string, token uint64, ttl time.Duration) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.maybePurgeLocked()

    holder, exists := instance.holders[name]
    if true == exists && true == instance.isActive(holder) && holder.token != token {
        return false
    }

    instance.holders[name] = inMemoryHolder{
        token:     token,
        expiresAt: instance.expiry(ttl),
    }

    return true
}

func (instance *InMemoryLocker) release(name string, token uint64) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    holder, exists := instance.holders[name]
    if false == exists || holder.token != token {
        return
    }

    delete(instance.holders, name)
}

func (instance *InMemoryLocker) refresh(name string, token uint64, ttl time.Duration) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    holder, exists := instance.holders[name]
    if false == exists || holder.token != token || false == instance.isActive(holder) {
        return false
    }

    instance.holders[name] = inMemoryHolder{
        token:     token,
        expiresAt: instance.expiry(ttl),
    }

    return true
}

func (instance *InMemoryLocker) maybePurgeLocked() {
    instance.purgeTicks++
    if instance.purgeTicks < inMemoryPurgeInterval {
        return
    }

    instance.purgeTicks = 0
    for name, holder := range instance.holders {
        if false == instance.isActive(holder) {
            delete(instance.holders, name)
        }
    }
}

func (instance *InMemoryLocker) isActive(holder inMemoryHolder) bool {
    if true == holder.expiresAt.IsZero() {
        return true
    }

    return instance.clock.Now().Before(holder.expiresAt)
}

func (instance *InMemoryLocker) expiry(ttl time.Duration) time.Time {
    if 0 >= ttl {
        return time.Time{}
    }

    return instance.clock.Now().Add(ttl)
}

type inMemoryLock struct {
    locker *InMemoryLocker
    name   string
    ttl    time.Duration
    token  uint64
}

func (instance *inMemoryLock) Acquire(runtimeInstance runtimecontract.Runtime) (bool, error) {
    return instance.locker.acquire(instance.name, instance.token, instance.ttl), nil
}

func (instance *inMemoryLock) Release(runtimeInstance runtimecontract.Runtime) error {
    instance.locker.release(instance.name, instance.token)
    return nil
}

func (instance *inMemoryLock) Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error {
    if 0 >= ttl {
        return exception.NewError("lock refresh ttl must be positive", map[string]any{"name": instance.name}, nil)
    }

    if false == instance.locker.refresh(instance.name, instance.token, ttl) {
        return exception.NewError("in-memory lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

var _ lockcontract.Locker = (*InMemoryLocker)(nil)
var _ lockcontract.Lock = (*inMemoryLock)(nil)
