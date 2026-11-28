package lock

import (
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

/* InMemoryLockerConfig is what an in-memory locker is built with; every zero value is the default. */
type InMemoryLockerConfig struct {
    /* Clock reads the time a lease is judged against: nil is the system clock. */
    Clock clockcontract.Clock

    /* PurgeInterval is how many acquisitions pass between two sweeps of the expired holders: 512 by default, at least 1, unlimited leaving the sweep to PurgeExpired. */
    PurgeInterval bound.Count
}

/* DefaultInMemoryLockerPurgeInterval is the PurgeInterval an InMemoryLockerConfig that leaves it out takes. */
const DefaultInMemoryLockerPurgeInterval = 512

var inMemoryLockerPurgeIntervalRule = bound.Rule[int]{
    Name:              "InMemoryLockerConfig.PurgeInterval",
    Default:           DefaultInMemoryLockerPurgeInterval,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* NewInMemoryLocker answers a locker holding its leases in the process, not shared by replicas, so it does not answer SharedLeaseLocker; a typed nil clock and a PurgeInterval its rule does not admit panic naming the field. */
func NewInMemoryLocker(config InMemoryLockerConfig) *InMemoryLocker {
    clockInstance, clockErr := clock.ResolveOptionalClock(config.Clock, "in-memory locker", "InMemoryLockerConfig.Clock")
    if nil != clockErr {
        exception.Panic(exception.FromError(clockErr))
    }

    purgeInterval, purgeIntervalErr := bound.Resolve(config.PurgeInterval, inMemoryLockerPurgeIntervalRule)
    if nil != purgeIntervalErr {
        exception.Panic(exception.FromError(purgeIntervalErr))
    }

    return &InMemoryLocker{
        clock:              clockInstance,
        holders:            make(map[string]inMemoryHolder),
        purgeInterval:      purgeInterval.Value,
        purgeIntervalUnlimited: purgeInterval.Unlimited,
    }
}

type InMemoryLocker struct {
    clock   clockcontract.Clock
    mutex   sync.Mutex
    holders map[string]inMemoryHolder

    /* atomic.Uint64 rather than a bare uint64: this field is not 8-byte aligned on a 32-bit build, where atomic.AddUint64 on a bare field panics, and the wrapper type carries its own alignment */
    counter    atomic.Uint64
    purgeTicks int

    purgeInterval      int
    purgeIntervalUnlimited bool
}

type inMemoryHolder struct {
    token     uint64
    expiresAt time.Time
}

/* CreateLock answers a lease lock, its ttl resolved through lockcontract.ResolveLeaseTtl here, so an absent one holds for 30s and an unlimited one is refused, and a holder that crashes lets the lock lapse. */
func (instance *InMemoryLocker) CreateLock(name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    if _, ttlErr := lockcontract.ResolveLeaseTtl(options.Ttl); nil != ttlErr {
        return nil, ttlErr
    }

    token := instance.counter.Add(1)

    return &inMemoryLock{
        locker: instance,
        name:   name,
        ttl:    options.Ttl,
        token:  token,
    }, nil
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
    if true == instance.purgeIntervalUnlimited {
        return
    }

    instance.purgeTicks++
    if instance.purgeTicks < instance.purgeInterval {
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
    return instance.clock.Now().Before(holder.expiresAt)
}

func (instance *InMemoryLocker) expiry(ttl time.Duration) time.Time {
    return instance.clock.Now().Add(ttl)
}

type inMemoryLock struct {
    locker *InMemoryLocker
    name   string
    ttl    bound.Duration
    token  uint64
}

func (instance *inMemoryLock) Acquire(runtimeInstance containercontract.Runtime) (bool, error) {
    ttl, ttlErr := lockcontract.ResolveLeaseTtl(instance.ttl)
    if nil != ttlErr {
        return false, ttlErr
    }

    return instance.locker.acquire(instance.name, instance.token, ttl), nil
}

func (instance *inMemoryLock) Release(runtimeInstance containercontract.Runtime) error {
    instance.locker.release(instance.name, instance.token)
    return nil
}

func (instance *inMemoryLock) Refresh(runtimeInstance containercontract.Runtime, options lockcontract.RefreshOptions) error {
    ttl := options.Ttl
    if true == ttl.IsAbsent() {
        ttl = instance.ttl
    }

    resolvedTtl, ttlErr := lockcontract.ResolveLeaseRefreshTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    if false == instance.locker.refresh(instance.name, instance.token, resolvedTtl) {
        return exception.NewError("in-memory lock is no longer held", map[string]any{"name": instance.name}, nil)
    }

    return nil
}

var _ lockcontract.Locker = (*InMemoryLocker)(nil)
var _ lockcontract.Lock = (*inMemoryLock)(nil)
