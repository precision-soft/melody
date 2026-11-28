package security

import (
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* failingCache answers every call with its failure, the cache that is down */
type failingCache struct {
    cachecontract.Cache
    failure error
}

func (instance *failingCache) Increment(_ containercontract.Runtime, _ string, _ int64, _ cachecontract.CounterOptions) (int64, error) {
    return 0, instance.failure
}

func (instance *failingCache) DeleteMultiple(_ containercontract.Runtime, _ []string) error {
    return instance.failure
}

/* deadlineFailingCache counts as the cache does and fails the deadline's read and write */
type deadlineFailingCache struct {
    cachecontract.Cache
    failure error
}

func (instance *deadlineFailingCache) Set(_ containercontract.Runtime, _ string, _ any, _ bound.Duration, _ cachecontract.SetOptions) error {
    return instance.failure
}

func (instance *deadlineFailingCache) Get(_ containercontract.Runtime, _ string, _ any) (bool, error) {
    return false, instance.failure
}

func newFrozenCacheStore(t *testing.T) (*CacheFailedAttemptStore, cachecontract.Cache, containercontract.Runtime, *clock.FrozenClock) {
    t.Helper()

    frozen := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    cacheInstance := newTestCache(t, frozen)
    runtimeInstance, _ := runtimeWithCache(t, cacheInstance)

    return NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Cache: cacheInstance, Clock: frozen}), cacheInstance, runtimeInstance, frozen
}

func incrementOrFail(t *testing.T, store securitycontract.FailedAttemptStore, runtimeInstance containercontract.Runtime, key string, window time.Duration) securitycontract.FailedAttemptCount {
    t.Helper()

    count, err := store.Increment(runtimeInstance, key, bound.Of(window))
    if nil != err {
        t.Fatalf("increment: %v", err)
    }

    return count
}

func TestCacheFailedAttemptStore_Increment_ArmsTheWindowOnce(t *testing.T) {
    store, _, runtimeInstance, frozen := newFrozenCacheStore(t)

    for attempt := 1; attempt <= 3; attempt++ {
        if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); attempt != count.Count {
            t.Fatalf("attempt %d: got count %d", attempt, count.Count)
        }

        frozen.Advance(10 * time.Second)
    }

    frozen.Advance(31 * time.Second)

    if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); 1 != count.Count || time.Minute != count.WindowRemaining {
        t.Fatalf("expected a fresh window once the first lapsed, got %+v", count)
    }
}

func TestCacheFailedAttemptStore_Increment_AnswersTheRemainderOfTheWindow(t *testing.T) {
    store, _, runtimeInstance, frozen := newFrozenCacheStore(t)

    if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); time.Minute != count.WindowRemaining {
        t.Fatalf("expected the opening count to answer the whole window, got %+v", count)
    }

    frozen.Advance(20 * time.Second)
    incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute)

    frozen.Advance(15 * time.Second)

    if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); 3 != count.Count || 25*time.Second != count.WindowRemaining {
        t.Fatalf("expected 25s left of the window the first count opened, got %+v", count)
    }
}

func TestCacheFailedAttemptStore_Increment_DeadlineKeyMissing_AnswersZeroRemaining(t *testing.T) {
    store, cacheInstance, runtimeInstance, _ := newFrozenCacheStore(t)

    incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute)

    if deleteErr := cacheInstance.Delete(runtimeInstance, store.counterKey("2fa:user-1")+FailedAttemptDeadlineKeySuffix); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); 2 != count.Count || 0 != count.WindowRemaining {
        t.Fatalf("expected the count kept and no remainder, got %+v", count)
    }
}

func TestCacheFailedAttemptStore_Increment_DeadlineAheadOfTheWindow_CappedAtTheWindow(t *testing.T) {
    store, cacheInstance, runtimeInstance, frozen := newFrozenCacheStore(t)

    incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute)

    farDeadline := frozen.Now().Add(time.Hour).UnixMilli()
    if setErr := cacheInstance.Set(runtimeInstance, store.counterKey("2fa:user-1")+FailedAttemptDeadlineKeySuffix, farDeadline, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    if count := incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute); time.Minute != count.WindowRemaining {
        t.Fatalf("expected the remainder capped at the window, got %+v", count)
    }
}

func TestCacheFailedAttemptStore_Increment_DeadlineFailure_AnsweredAsTheError(t *testing.T) {
    _, cacheInstance, runtimeInstance, frozen := newFrozenCacheStore(t)
    deadlineErr := errors.New("cache is down")
    store := NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Cache: &deadlineFailingCache{Cache: cacheInstance, failure: deadlineErr}, Clock: frozen})

    if _, err := store.Increment(runtimeInstance, "2fa:user-1", bound.Of(time.Minute)); false == errors.Is(err, deadlineErr) {
        t.Fatalf("expected the deadline's write failure answered, got %v", err)
    }

    if _, err := store.Increment(runtimeInstance, "2fa:user-1", bound.Of(time.Minute)); false == errors.Is(err, deadlineErr) {
        t.Fatalf("expected the deadline's read failure answered, got %v", err)
    }
}

func TestCacheFailedAttemptStore_KeyCarriesNoIdentifier(t *testing.T) {
    store := NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{})

    key := store.counterKey("2fa:alice@example.com")
    if true == strings.Contains(key, "alice") || len(DefaultFailedAttemptKeyPrefix)+64 != len(key) {
        t.Fatalf("expected the prefix and a sha-256 hex digest, got %q", key)
    }

    if key == store.counterKey("2fa:bob@example.com") {
        t.Fatal("expected two accounts under two keys")
    }
}

func TestCacheFailedAttemptStore_DefaultKeyPrefix(t *testing.T) {
    if "melody:2fa:failed:" != DefaultFailedAttemptKeyPrefix {
        t.Fatalf("unexpected default prefix %q", DefaultFailedAttemptKeyPrefix)
    }

    if false == strings.HasPrefix(NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{}).counterKey("k"), DefaultFailedAttemptKeyPrefix) {
        t.Fatal("expected the default prefix on an empty KeyPrefix")
    }

    if false == strings.HasPrefix(NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{KeyPrefix: "app:"}).counterKey("k"), "app:") {
        t.Fatal("expected the configured prefix")
    }
}

func TestCacheFailedAttemptStore_Reset_ForgetsBothKeys(t *testing.T) {
    store, cacheInstance, runtimeInstance, _ := newFrozenCacheStore(t)

    incrementOrFail(t, store, runtimeInstance, "2fa:user-1", time.Minute)
    incrementOrFail(t, store, runtimeInstance, "2fa:user-2", time.Minute)

    if resetErr := store.Reset(runtimeInstance, "2fa:user-1"); nil != resetErr {
        t.Fatalf("reset: %v", resetErr)
    }

    for _, key := range []string{store.counterKey("2fa:user-1"), store.counterKey("2fa:user-1") + FailedAttemptDeadlineKeySuffix} {
        if found, _ := cacheInstance.Has(runtimeInstance, key); true == found {
            t.Fatalf("expected %s forgotten", key)
        }
    }

    if found, _ := cacheInstance.Has(runtimeInstance, store.counterKey("2fa:user-2")); false == found {
        t.Fatal("expected the other account's count kept")
    }
}

func TestCacheFailedAttemptStore_AbsentWindow_Refused(t *testing.T) {
    store, _, runtimeInstance, _ := newFrozenCacheStore(t)

    _, err := store.Increment(runtimeInstance, "2fa:user-1", bound.Duration{})
    expectErrorNamingField(t, err, "FailedAttemptStore.Increment.window")
}

func TestCacheFailedAttemptStore_UnlimitedWindow_Refused(t *testing.T) {
    store, _, runtimeInstance, _ := newFrozenCacheStore(t)

    _, unlimitedErr := store.Increment(runtimeInstance, "2fa:user-1", bound.UnlimitedDuration())
    expectErrorNamingField(t, unlimitedErr, "FailedAttemptStore.Increment.window")

    _, shortErr := store.Increment(runtimeInstance, "2fa:user-1", bound.Of(999*time.Millisecond))
    expectErrorNamingField(t, shortErr, "FailedAttemptStore.Increment.window")
}

func TestCacheFailedAttemptStore_CacheFailure_AnsweredAsTheError(t *testing.T) {
    cacheErr := errors.New("cache is down")
    store := NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Cache: &failingCache{failure: cacheErr}})
    runtimeInstance := newTestRuntime()

    if _, err := store.Increment(runtimeInstance, "2fa:user-1", bound.Of(time.Minute)); false == errors.Is(err, cacheErr) {
        t.Fatalf("expected the increment's failure answered, got %v", err)
    }

    if err := store.Reset(runtimeInstance, "2fa:user-1"); false == errors.Is(err, cacheErr) {
        t.Fatalf("expected the reset's failure answered, got %v", err)
    }
}

func TestNewCacheFailedAttemptStore_TypedNilFields_PanicNamingField(t *testing.T) {
    var typedNilCache *failingCache
    var typedNilClock *clock.FrozenClock

    expectPanicNamingField(t, "CacheFailedAttemptStoreConfig.Cache", func() {
        NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Cache: typedNilCache})
    })

    expectPanicNamingField(t, "CacheFailedAttemptStoreConfig.Clock", func() {
        NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Clock: typedNilClock})
    })
}
