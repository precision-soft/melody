package security

import (
    "crypto/sha256"
    "encoding/hex"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* DefaultFailedAttemptKeyPrefix is the namespace of every failed attempt counter unless CacheFailedAttemptStoreConfig.KeyPrefix names another. */
const DefaultFailedAttemptKeyPrefix = "melody:2fa:failed:"

/* FailedAttemptDeadlineKeySuffix names the key beside each counter that holds its window's deadline, since a cache counter answers its count and not its remaining ttl. */
const FailedAttemptDeadlineKeySuffix = ":until"

/* CacheFailedAttemptStoreConfig is what the cache-backed store declares at construction; the zero value is the application's cache under DefaultFailedAttemptKeyPrefix. */
type CacheFailedAttemptStoreConfig struct {
    /* Cache is where the counters live; nil is the application's cache, read from the runtime of each call. */
    Cache cachecontract.Cache

    /* KeyPrefix is the namespace of every counter, DefaultFailedAttemptKeyPrefix when empty. */
    KeyPrefix string

    /* Clock is where the window's deadline is stamped and read; nil is the system clock. */
    Clock clockcontract.Clock
}

/* NewCacheFailedAttemptStore builds the FailedAttemptStore over a cache: a fixed-window counter per key, and beside it a second key holding the window's deadline, written by the count that opens the window. The two writes are not atomic: when the deadline key is missing, because the opening count's write of it failed or has not landed yet, or the cache evicted it, the store answers a zero remainder and the count stands. A typed nil Cache or Clock panics naming the field. */
func NewCacheFailedAttemptStore(config CacheFailedAttemptStoreConfig) *CacheFailedAttemptStore {
    refuseTypedNilField("failed attempt store cache is a typed nil", "CacheFailedAttemptStoreConfig.Cache", "NewCacheFailedAttemptStore", config.Cache)
    refuseTypedNilField("failed attempt store clock is a typed nil", "CacheFailedAttemptStoreConfig.Clock", "NewCacheFailedAttemptStore", config.Clock)

    keyPrefix := config.KeyPrefix
    if "" == keyPrefix {
        keyPrefix = DefaultFailedAttemptKeyPrefix
    }

    clockInstance := config.Clock
    if nil == clockInstance {
        clockInstance = clock.NewSystemClock()
    }

    return &CacheFailedAttemptStore{
        cache:     config.Cache,
        keyPrefix: keyPrefix,
        clock:     clockInstance,
    }
}

/* CacheFailedAttemptStore counts failed attempts in a cache, shared between instances exactly when the cache is. A cache Clear forgets every count, and a backend that evicts at its ceiling may forget one. */
type CacheFailedAttemptStore struct {
    cache     cachecontract.Cache
    keyPrefix string
    clock     clockcontract.Clock
}

func (instance *CacheFailedAttemptStore) Increment(
    runtimeInstance containercontract.Runtime,
    key string,
    window bound.Duration,
) (securitycontract.FailedAttemptCount, error) {
    windowValue, windowErr := securitycontract.ResolveFailedAttemptWindow(window)
    if nil != windowErr {
        return securitycontract.FailedAttemptCount{}, windowErr
    }

    cacheInstance := instance.cacheOf(runtimeInstance)
    counterKey := instance.counterKey(key)
    deadlineKey := counterKey + FailedAttemptDeadlineKeySuffix

    count, incrementErr := cacheInstance.Increment(
        runtimeInstance,
        counterKey,
        1,
        cachecontract.CounterOptions{Ttl: bound.Of(windowValue), RefreshTtl: false},
    )
    if nil != incrementErr {
        return securitycontract.FailedAttemptCount{}, incrementErr
    }

    now := instance.clock.Now()

    if 1 == count {
        deadline := now.Add(windowValue)
        if setErr := cacheInstance.Set(runtimeInstance, deadlineKey, deadline.UnixMilli(), bound.Of(windowValue)); nil != setErr {
            return securitycontract.FailedAttemptCount{}, setErr
        }

        return securitycontract.FailedAttemptCount{Count: 1, WindowRemaining: windowValue}, nil
    }

    var deadlineMilliseconds int64
    found, getErr := cacheInstance.Get(runtimeInstance, deadlineKey, &deadlineMilliseconds)
    if nil != getErr {
        return securitycontract.FailedAttemptCount{}, getErr
    }

    return securitycontract.FailedAttemptCount{
        Count:           int(count),
        WindowRemaining: remainingUntil(found, deadlineMilliseconds, now, windowValue),
    }, nil
}

func (instance *CacheFailedAttemptStore) Reset(runtimeInstance containercontract.Runtime, key string) error {
    counterKey := instance.counterKey(key)

    return instance.cacheOf(runtimeInstance).DeleteMultiple(
        runtimeInstance,
        []string{counterKey, counterKey + FailedAttemptDeadlineKeySuffix},
    )
}

func (instance *CacheFailedAttemptStore) cacheOf(runtimeInstance containercontract.Runtime) cachecontract.Cache {
    if false == internal.IsNilInterface(instance.cache) {
        return instance.cache
    }

    return runtime.MustFromRuntime[cachecontract.Cache](runtimeInstance, cache.ServiceCache)
}

/* counterKey hashes the key, so no account identifier is written into the cache's key space */
func (instance *CacheFailedAttemptStore) counterKey(key string) string {
    digest := sha256.Sum256([]byte(key))

    return instance.keyPrefix + hex.EncodeToString(digest[:])
}

/* remainingUntil is capped at the window, since a deadline stamped by another instance's clock may run ahead of this one */
func remainingUntil(found bool, deadlineMilliseconds int64, now time.Time, window time.Duration) time.Duration {
    if false == found {
        return 0
    }

    remaining := time.UnixMilli(deadlineMilliseconds).Sub(now)
    if 0 > remaining {
        return 0
    }

    if window < remaining {
        return window
    }

    return remaining
}

var _ securitycontract.FailedAttemptStore = (*CacheFailedAttemptStore)(nil)
