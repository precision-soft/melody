package cache

import (
    "context"
    "errors"
    "sync"
    "sync/atomic"
    "testing"
    "time"
)

func TestRemember_ReturnsCachedValueWithoutCallingCallback(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheInstance := NewManager(backend, NewJsonSerializer())

    setErr := cacheInstance.Set("k", "v", 0)
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    called := false
    value, rememberErr := Remember(
        cacheInstance,
        "k",
        time.Second,
        func(ctx context.Context) (any, error) {
            called = true
            return "new", nil
        },
        nil,
    )
    if nil != rememberErr {
        t.Fatalf("remember error: %v", rememberErr)
    }
    if true == called {
        t.Fatalf("expected callback not to be called on cache hit")
    }
    if "v" != value.(string) {
        t.Fatalf("expected cached value")
    }
}

func TestRemember_CallsCallbackAndStoresValue(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheInstance := NewManager(backend, NewJsonSerializer())

    called := false
    value, rememberErr := Remember(
        cacheInstance,
        "k",
        time.Second,
        func(ctx context.Context) (any, error) {
            called = true
            return "computed", nil
        },
        nil,
    )
    if nil != rememberErr {
        t.Fatalf("remember error: %v", rememberErr)
    }
    if false == called {
        t.Fatalf("expected callback to be called on cache miss")
    }
    if "computed" != value.(string) {
        t.Fatalf("unexpected value")
    }

    storedValue, exists, getErr := cacheInstance.Get("k")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists {
        t.Fatalf("expected value to be stored")
    }
    if "computed" != storedValue.(string) {
        t.Fatalf("expected stored value")
    }
}

func TestRemember_PropagatesCallbackErrorAndDoesNotStore(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheInstance := NewManager(backend, NewJsonSerializer())

    expectedErr := errors.New("callback error")

    _, rememberErr := Remember(
        cacheInstance,
        "k",
        time.Second,
        func(ctx context.Context) (any, error) {
            return nil, expectedErr
        },
        nil,
    )
    if nil == rememberErr {
        t.Fatalf("expected error")
    }
    if expectedErr.Error() != rememberErr.Error() {
        t.Fatalf("expected callback error to propagate")
    }

    _, exists, getErr := cacheInstance.Get("k")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected value not to be stored")
    }
}

func TestRemember_ZeroTtlActsAsForever(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        10,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheInstance := NewManager(backend, NewJsonSerializer())

    _, rememberErr := Remember(
        cacheInstance,
        "k",
        0,
        func(ctx context.Context) (any, error) {
            return "v", nil
        },
        nil,
    )
    if nil != rememberErr {
        t.Fatalf("remember error: %v", rememberErr)
    }

    clockInstance.now = time.Unix(10+3600, 0)

    value, exists, getErr := cacheInstance.Get("k")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists {
        t.Fatalf("expected value to still exist with ttl=0")
    }
    if "v" != value.(string) {
        t.Fatalf("unexpected value")
    }
}

func TestRemember_ProtectAgainstStampede_ExecutesCallbackOnce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        100,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    concurrency := 50

    var waitGroup sync.WaitGroup
    waitGroup.Add(concurrency)

    errorChannel := make(chan error, concurrency)

    for index := 0; index < concurrency; index++ {
        go func() {
            defer waitGroup.Done()

            value, err := Remember(
                cacheManager,
                "product.1",
                time.Minute,
                callback,
                nil,
            )
            if nil != err {
                errorChannel <- err
                return
            }
            if "value" != value {
                errorChannel <- errors.New("unexpected value")
                return
            }
        }()
    }

    deadline := time.NewTimer(2 * time.Second)
    defer deadline.Stop()

    for {
        if 1 == atomic.LoadInt64(&callbackCalls) {
            break
        }

        select {
        case <-time.After(5 * time.Millisecond):
            continue
        case <-deadline.C:
            t.Fatalf("expected callback to be called once before release, got %d", atomic.LoadInt64(&callbackCalls))
        }
    }

    close(releaseCallbackChannel)
    waitGroup.Wait()
    close(errorChannel)

    for err := range errorChannel {
        if nil != err {
            t.Fatalf("unexpected error: %v", err)
        }
    }

    if 1 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected callback to be called once, got %d", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_StampedeProtectionDisabled_AllowsParallelCallbackCalls(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        100,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    concurrency := 50

    var waitGroup sync.WaitGroup
    waitGroup.Add(concurrency)

    option := NewDefaultRememberOption().
        WithStampedeProtectionEnabled(false).
        WithWaitTimeout(-1).
        WithCancelable(false)

    for index := 0; index < concurrency; index++ {
        go func() {
            defer waitGroup.Done()

            _, _ = Remember(
                cacheManager,
                "product.2",
                time.Minute,
                callback,
                option,
            )
        }()
    }

    deadline := time.NewTimer(2 * time.Second)
    defer deadline.Stop()

    for {
        if 2 <= atomic.LoadInt64(&callbackCalls) {
            break
        }

        select {
        case <-time.After(5 * time.Millisecond):
            continue
        case <-deadline.C:
            t.Fatalf("expected callback to be called at least twice before release, got %d", atomic.LoadInt64(&callbackCalls))
        }
    }

    close(releaseCallbackChannel)
    waitGroup.Wait()

    if 2 > atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected callback to be called at least twice, got %d", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_WaitTimeoutIsPerCaller_DoesNotPoisonInFlightCall(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        100,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    startTime := time.Now()

    resultChannelA := make(chan error, 1)
    go func() {
        _, err := Remember(
            cacheManager,
            "product.3",
            time.Minute,
            callback,
            NewDefaultRememberOption().
                WithWaitTimeout(-1).
                WithCancelable(false),
        )
        resultChannelA <- err
    }()

    time.Sleep(10 * time.Millisecond)

    resultChannelB := make(chan error, 1)
    go func() {
        _, err := Remember(
            cacheManager,
            "product.3",
            time.Minute,
            callback,
            NewDefaultRememberOption().
                WithWaitTimeout(100*time.Millisecond).
                WithCancelable(false),
        )
        resultChannelB <- err
    }()

    time.Sleep(20 * time.Millisecond)

    resultChannelC := make(chan any, 1)
    resultChannelCErr := make(chan error, 1)
    go func() {
        value, err := Remember(
            cacheManager,
            "product.3",
            time.Minute,
            callback,
            NewDefaultRememberOption().
                WithWaitTimeout(150*time.Millisecond).
                WithCancelable(false),
        )
        resultChannelC <- value
        resultChannelCErr <- err
    }()

    elapsedUntilRelease := time.Since(startTime)
    if 120*time.Millisecond > elapsedUntilRelease {
        time.Sleep(120*time.Millisecond - elapsedUntilRelease)
    }

    close(releaseCallbackChannel)

    errA := <-resultChannelA
    if nil != errA {
        t.Fatalf("unexpected error for caller A: %v", errA)
    }

    errB := <-resultChannelB
    if nil == errB {
        t.Fatalf("expected timeout error for caller B")
    }

    errC := <-resultChannelCErr
    if nil != errC {
        t.Fatalf("unexpected error for caller C: %v", errC)
    }

    valueC := <-resultChannelC
    if "value" != valueC {
        t.Fatalf("unexpected value for caller C")
    }

    if 1 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected callback to be called once, got %d", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_CancelableLateJoinerIsNotPoisonedByCanceledCall(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        100,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
    )

    callbackStarted := make(chan struct{})
    callbackCanceled := make(chan struct{})
    holdLeader := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        call := atomic.AddInt64(&callbackCalls, 1)
        if 1 == call {
            close(callbackStarted)
            <-ctx.Done()
            close(callbackCanceled)
            <-holdLeader

            return nil, ctx.Err()
        }

        return "fresh", nil
    }

    timedOutChannel := make(chan error, 1)
    go func() {
        _, err := Remember(
            cacheManager,
            "product.late-joiner",
            time.Minute,
            callback,
            NewDefaultRememberOption().
                WithWaitTimeout(30*time.Millisecond).
                WithCancelable(true),
        )
        timedOutChannel <- err
    }()

    <-callbackStarted

    timedOutErr := <-timedOutChannel
    if nil == timedOutErr {
        t.Fatalf("expected the only waiter to time out")
    }

    <-callbackCanceled

    joinerValueChannel := make(chan any, 1)
    joinerErrChannel := make(chan error, 1)
    go func() {
        value, err := Remember(
            cacheManager,
            "product.late-joiner",
            time.Minute,
            callback,
            NewDefaultRememberOption().
                WithWaitTimeout(2*time.Second).
                WithCancelable(true),
        )
        joinerValueChannel <- value
        joinerErrChannel <- err
    }()

    time.Sleep(20 * time.Millisecond)
    close(holdLeader)

    joinerValue := <-joinerValueChannel
    joinerErr := <-joinerErrChannel

    if nil != joinerErr {
        t.Fatalf("late joiner inherited the canceled call's poison: %v", joinerErr)
    }

    if "fresh" != joinerValue {
        t.Fatalf("unexpected late joiner value: %v", joinerValue)
    }

    if 2 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected the late joiner to lead a fresh computation, callback calls: %d", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_CancelableGroupIsSeparatedFromNonCancelableGroup(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(
        100,
        time.Hour,
        clockInstance,
    )
    defer backend.Close()

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
    )

    releaseCallbackNonCancelableChannel := make(chan struct{})
    releaseCallbackCancelableChannel := make(chan struct{})

    var nonCancelableCalls int64
    var cancelableCalls int64

    nonCancelableCallback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&nonCancelableCalls, 1)
        <-releaseCallbackNonCancelableChannel
        return "nonCancelable", nil
    }

    cancelableCallback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&cancelableCalls, 1)
        <-releaseCallbackCancelableChannel
        return "cancelable", nil
    }

    resultChannelNonCancelable := make(chan any, 1)
    resultChannelCancelable := make(chan any, 1)

    go func() {
        value, _ := Remember(
            cacheManager,
            "product.4",
            time.Minute,
            nonCancelableCallback,
            NewDefaultRememberOption().
                WithWaitTimeout(-1).
                WithCancelable(false),
        )
        resultChannelNonCancelable <- value
    }()

    go func() {
        value, _ := Remember(
            cacheManager,
            "product.4",
            time.Minute,
            cancelableCallback,
            NewDefaultRememberOption().
                WithWaitTimeout(-1).
                WithCancelable(true),
        )
        resultChannelCancelable <- value
    }()

    deadline := time.NewTimer(2 * time.Second)
    defer deadline.Stop()

    for {
        if 1 == atomic.LoadInt64(&nonCancelableCalls) && 1 == atomic.LoadInt64(&cancelableCalls) {
            break
        }

        select {
        case <-time.After(5 * time.Millisecond):
            continue
        case <-deadline.C:
            t.Fatalf("expected both callbacks to be called once, got nonCancelable=%d cancelable=%d", atomic.LoadInt64(&nonCancelableCalls), atomic.LoadInt64(&cancelableCalls))
        }
    }

    close(releaseCallbackNonCancelableChannel)
    close(releaseCallbackCancelableChannel)

    valueNonCancelable := <-resultChannelNonCancelable
    valueCancelable := <-resultChannelCancelable

    if "nonCancelable" != valueNonCancelable {
        t.Fatalf("unexpected nonCancelable value")
    }
    if "cancelable" != valueCancelable {
        t.Fatalf("unexpected cancelable value")
    }
}

/* @info the guard reads through the interface: a typed-nil Cache is a non-nil interface that passed the plain comparison and panicked on the first method call, on the request path, in place of the error the refusal promises */
func TestRemember_RefusesATypedNilCache(t *testing.T) {
    var typedNilManager *Manager

    _, rememberErr := Remember(
        typedNilManager,
        "k",
        time.Second,
        func(ctx context.Context) (any, error) {
            return "v", nil
        },
        nil,
    )
    if nil == rememberErr {
        t.Fatalf("expected the typed-nil cache to be refused")
    }
    if "cache instance is nil" != rememberErr.Error() {
        t.Fatalf("expected the nil-cache refusal, got: %v", rememberErr)
    }
}

/* @info the zero-value option is constructible from outside the package and silently disarmed the stampede protection it never asked to configure; it reads as the constructor defaults instead, so the leader is joined rather than raced */
func TestRemember_ZeroValueOptionKeepsStampedeProtection(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    cacheManager := NewManager(backend, NewJsonSerializer())

    releaseCallbackChannel := make(chan struct{})
    leaderStartedChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        if 1 == atomic.AddInt64(&callbackCalls, 1) {
            close(leaderStartedChannel)
        }
        <-releaseCallbackChannel
        return "value", nil
    }

    leaderResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(cacheManager, "zero.value.option", time.Minute, callback, nil)
        leaderResultChannel <- err
    }()

    <-leaderStartedChannel

    joinerResultChannel := make(chan error, 1)
    joinerValueChannel := make(chan any, 1)
    go func() {
        value, err := Remember(cacheManager, "zero.value.option", time.Minute, callback, &RememberOption{})
        joinerValueChannel <- value
        joinerResultChannel <- err
    }()

    time.Sleep(20 * time.Millisecond)

    close(releaseCallbackChannel)

    if leaderErr := <-leaderResultChannel; nil != leaderErr {
        t.Fatalf("leader error: %v", leaderErr)
    }
    if joinerErr := <-joinerResultChannel; nil != joinerErr {
        t.Fatalf("joiner error: %v", joinerErr)
    }
    if "value" != <-joinerValueChannel {
        t.Fatalf("unexpected joiner value")
    }

    if 1 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected the zero-value option to join the in-flight leader, callback calls: %d", atomic.LoadInt64(&callbackCalls))
    }
}

/* @info a payload the serializer cannot decode is a miss, not a failure: the callback recomputes and its Set overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever */
func TestRemember_RecomputesOverACorruptPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    cacheManager := NewManager(backend, NewJsonSerializer())

    if setErr := backend.Set("poisoned", []byte("{not json"), 0); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    value, rememberErr := Remember(
        cacheManager,
        "poisoned",
        0,
        func(ctx context.Context) (any, error) {
            return "healed", nil
        },
        nil,
    )
    if nil != rememberErr {
        t.Fatalf("expected the corrupt payload to be recomputed, got: %v", rememberErr)
    }
    if "healed" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    storedValue, exists, getErr := cacheManager.Get("poisoned")
    if nil != getErr {
        t.Fatalf("expected the corrupt payload to have been overwritten, got: %v", getErr)
    }
    if false == exists || "healed" != storedValue.(string) {
        t.Fatalf("expected the healed value in the cache, got exists=%v value=%v", exists, storedValue)
    }
}

/* @info a typed-nil error from the callback reads as the success it means: boxed into a non-nil interface it was memoized as the flight's failure, handed to every waiter, and panicked the first one that rendered it */
func TestRemember_CallbackTypedNilErrorIsSuccess(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    cacheManager := NewManager(backend, NewJsonSerializer())

    value, rememberErr := Remember(
        cacheManager,
        "typed.nil.callback",
        time.Minute,
        func(ctx context.Context) (any, error) {
            var typedNil *testRememberError
            return "computed", typedNil
        },
        nil,
    )
    if nil != rememberErr {
        t.Fatalf("expected the typed-nil callback error to read as success, got: %v", rememberErr)
    }
    if "computed" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    storedValue, exists, getErr := cacheManager.Get("typed.nil.callback")
    if nil != getErr || false == exists || "computed" != storedValue.(string) {
        t.Fatalf("expected the computed value to have been stored, got exists=%v value=%v err=%v", exists, storedValue, getErr)
    }
}

type testRememberError struct{}

func (instance *testRememberError) Error() string {
    return "test remember error"
}

type testValueKindCache struct {
    inner *Manager
}

func (instance testValueKindCache) Get(key string) (any, bool, error) {
    return instance.inner.Get(key)
}

func (instance testValueKindCache) Set(key string, value any, ttl time.Duration) error {
    return instance.inner.Set(key, value, ttl)
}

func (instance testValueKindCache) Delete(key string) error {
    return instance.inner.Delete(key)
}

func (instance testValueKindCache) Has(key string) (bool, error) {
    return instance.inner.Has(key)
}

func (instance testValueKindCache) Clear() error {
    return instance.inner.Clear()
}

func (instance testValueKindCache) Many(keys []string) (map[string]any, error) {
    return instance.inner.Many(keys)
}

func (instance testValueKindCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return instance.inner.SetMultiple(items, ttl)
}

func (instance testValueKindCache) DeleteMultiple(keys []string) error {
    return instance.inner.DeleteMultiple(keys)
}

func (instance testValueKindCache) Increment(key string, delta int64) (int64, error) {
    return instance.inner.Increment(key, delta)
}

func (instance testValueKindCache) Decrement(key string, delta int64) (int64, error) {
    return instance.inner.Decrement(key, delta)
}

func (instance testValueKindCache) Close() error {
    return instance.inner.Close()
}

/* @info a value-kind Cache has no address to tell two instances apart, so it gets no coalescing at all: one shared flight would hand a caller the value computed for somebody else's cache, and losing the stampede optimization is the price of never losing the answer */
func TestRemember_ValueKindCacheDoesNotCoalesce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    valueKindCache := testValueKindCache{inner: NewManager(backend, NewJsonSerializer())}

    releaseCallbackChannel := make(chan struct{})
    firstStartedChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        if 1 == atomic.AddInt64(&callbackCalls, 1) {
            close(firstStartedChannel)
        }
        <-releaseCallbackChannel
        return "value", nil
    }

    firstResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(valueKindCache, "value.kind", time.Minute, callback, nil)
        firstResultChannel <- err
    }()

    <-firstStartedChannel

    secondResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(valueKindCache, "value.kind", time.Minute, callback, nil)
        secondResultChannel <- err
    }()

    deadline := time.NewTimer(2 * time.Second)
    defer deadline.Stop()

    for {
        if 2 <= atomic.LoadInt64(&callbackCalls) {
            break
        }

        select {
        case <-time.After(5 * time.Millisecond):
            continue
        case <-deadline.C:
            close(releaseCallbackChannel)
            <-firstResultChannel
            <-secondResultChannel
            t.Fatalf("expected the second caller to run its own callback instead of coalescing, callback calls: %d", atomic.LoadInt64(&callbackCalls))
        }
    }

    close(releaseCallbackChannel)

    if firstErr := <-firstResultChannel; nil != firstErr {
        t.Fatalf("first caller error: %v", firstErr)
    }
    if secondErr := <-secondResultChannel; nil != secondErr {
        t.Fatalf("second caller error: %v", secondErr)
    }
}

type testPanickingSetCache struct {
    inner *Manager
}

func (instance *testPanickingSetCache) Get(key string) (any, bool, error) {
    return instance.inner.Get(key)
}

func (instance *testPanickingSetCache) Set(key string, value any, ttl time.Duration) error {
    panic("backend exploded")
}

func (instance *testPanickingSetCache) Delete(key string) error {
    return instance.inner.Delete(key)
}

func (instance *testPanickingSetCache) Has(key string) (bool, error) {
    return instance.inner.Has(key)
}

func (instance *testPanickingSetCache) Clear() error {
    return instance.inner.Clear()
}

func (instance *testPanickingSetCache) Many(keys []string) (map[string]any, error) {
    return instance.inner.Many(keys)
}

func (instance *testPanickingSetCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return instance.inner.SetMultiple(items, ttl)
}

func (instance *testPanickingSetCache) DeleteMultiple(keys []string) error {
    return instance.inner.DeleteMultiple(keys)
}

func (instance *testPanickingSetCache) Increment(key string, delta int64) (int64, error) {
    return instance.inner.Increment(key, delta)
}

func (instance *testPanickingSetCache) Decrement(key string, delta int64) (int64, error) {
    return instance.inner.Decrement(key, delta)
}

func (instance *testPanickingSetCache) Close() error {
    return instance.inner.Close()
}

/* @info the callback's own panics are recovered inside the safe wrapper, so what the leader's recover catches is the cache side, and the fabricated error says so instead of blaming a callback that never misbehaved */
func TestRemember_CacheSidePanicIsNotBlamedOnTheCallback(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    panickingCache := &testPanickingSetCache{inner: NewManager(backend, NewJsonSerializer())}

    _, rememberErr := Remember(
        panickingCache,
        "panicking.set",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return "computed", nil
        },
        nil,
    )
    if nil == rememberErr {
        t.Fatalf("expected the cache-side panic to surface as an error")
    }
    if "cache remember cache access panicked" != rememberErr.Error() {
        t.Fatalf("expected the cache-side panic message, got: %v", rememberErr)
    }
}
