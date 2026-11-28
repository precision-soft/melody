package cache

import (
    "context"
    "errors"
    "math"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

func TestRemember_ReturnsCachedValueWithoutCallingCallback(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheInstance := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    setErr := cacheInstance.Set(testRuntime(), "k", "v", bound.UnlimitedDuration(), cachecontract.SetOptions{})
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    called := false
    value, rememberErr := Remember(
        For(testRuntime(), cacheInstance),
        "k",
        RememberOptions{Ttl: bound.Of(time.Second)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            called = true
            return "new", nil
        },
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheInstance := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    called := false
    value, rememberErr := Remember(
        For(testRuntime(), cacheInstance),
        "k",
        RememberOptions{Ttl: bound.Of(time.Second)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            called = true
            return "computed", nil
        },
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

    storedValue, exists, getErr := Get[any](For(testRuntime(), cacheInstance), "k")
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheInstance := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    expectedErr := errors.New("callback error")

    _, rememberErr := Remember(
        For(testRuntime(), cacheInstance),
        "k",
        RememberOptions{Ttl: bound.Of(time.Second)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return nil, expectedErr
        },
    )
    if nil == rememberErr {
        t.Fatalf("expected error")
    }
    if expectedErr.Error() != rememberErr.Error() {
        t.Fatalf("expected callback error to propagate")
    }

    _, exists, getErr := Get[any](For(testRuntime(), cacheInstance), "k")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected value not to be stored")
    }
}

func TestRemember_AnUnlimitedTtlKeepsTheValue(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheInstance := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    _, rememberErr := Remember(
        For(testRuntime(), cacheInstance),
        "k",
        RememberOptions{Ttl: bound.UnlimitedDuration()},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "v", nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember error: %v", rememberErr)
    }

    clockInstance.now = time.Unix(10+3600, 0)

    value, exists, getErr := Get[any](For(testRuntime(), cacheInstance), "k")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists {
        t.Fatalf("expected the value to still exist under an unlimited ttl")
    }
    if "v" != value.(string) {
        t.Fatalf("unexpected value")
    }
}

func TestRemember_ProtectAgainstStampede_ExecutesCallbackOnce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
        ManagerConfig{},
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
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
                For(testRuntime(), cacheManager),
                "product.1",
                RememberOptions{Ttl: bound.Of(time.Minute)},
                callback,
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
        ManagerConfig{},
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    concurrency := 50

    var waitGroup sync.WaitGroup
    waitGroup.Add(concurrency)

    config := RememberOptions{
        Ttl:                        bound.Of(time.Minute),
        StampedeProtectionDisabled: true,
        WaitTimeout:                bound.UnlimitedDuration(),
        Detached:                   true,
    }

    for index := 0; index < concurrency; index++ {
        go func() {
            defer waitGroup.Done()

            _, _ = Remember(
                For(testRuntime(), cacheManager),
                "product.2",
                config,
                callback,
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
        ManagerConfig{},
    )

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    startTime := time.Now()

    resultChannelA := make(chan error, 1)
    go func() {
        _, err := Remember(
            For(testRuntime(), cacheManager),
            "product.3",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.UnlimitedDuration(), Detached: true},
            callback,
        )
        resultChannelA <- err
    }()

    time.Sleep(10 * time.Millisecond)

    resultChannelB := make(chan error, 1)
    go func() {
        _, err := Remember(
            For(testRuntime(), cacheManager),
            "product.3",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(100 * time.Millisecond), Detached: true},
            callback,
        )
        resultChannelB <- err
    }()

    time.Sleep(20 * time.Millisecond)

    resultChannelC := make(chan any, 1)
    resultChannelCErr := make(chan error, 1)
    go func() {
        value, err := Remember(
            For(testRuntime(), cacheManager),
            "product.3",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(150 * time.Millisecond), Detached: true},
            callback,
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
        ManagerConfig{},
    )

    callbackStarted := make(chan struct{})
    callbackCanceled := make(chan struct{})
    holdLeader := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        call := atomic.AddInt64(&callbackCalls, 1)
        if 1 == call {
            close(callbackStarted)
            <-runtimeInstance.Context().Done()
            close(callbackCanceled)
            <-holdLeader

            return nil, runtimeInstance.Context().Err()
        }

        return "fresh", nil
    }

    timedOutChannel := make(chan error, 1)
    go func() {
        _, err := Remember(
            For(testRuntime(), cacheManager),
            "product.late-joiner",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(30 * time.Millisecond)},
            callback,
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
            For(testRuntime(), cacheManager),
            "product.late-joiner",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(2 * time.Second)},
            callback,
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

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(
        backend,
        NewJsonSerializer(),
        ManagerConfig{},
    )

    releaseCallbackNonCancelableChannel := make(chan struct{})
    releaseCallbackCancelableChannel := make(chan struct{})

    var nonCancelableCalls int64
    var cancelableCalls int64

    nonCancelableCallback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&nonCancelableCalls, 1)
        <-releaseCallbackNonCancelableChannel
        return "nonCancelable", nil
    }

    cancelableCallback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&cancelableCalls, 1)
        <-releaseCallbackCancelableChannel
        return "cancelable", nil
    }

    resultChannelNonCancelable := make(chan any, 1)
    resultChannelCancelable := make(chan any, 1)

    go func() {
        value, _ := Remember(
            For(testRuntime(), cacheManager),
            "product.4",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.UnlimitedDuration(), Detached: true},
            nonCancelableCallback,
        )
        resultChannelNonCancelable <- value
    }()

    go func() {
        value, _ := Remember(
            For(testRuntime(), cacheManager),
            "product.4",
            RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.UnlimitedDuration()},
            cancelableCallback,
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

/* the guard reads through the interface: a typed-nil Cache is a non-nil interface that passed the plain comparison and panicked on the first method call, on the request path, in place of the error the refusal promises */
func TestRemember_RefusesATypedNilCache(t *testing.T) {
    var typedNilManager *Manager

    _, rememberErr := Remember(
        For(testRuntime(), typedNilManager),
        "k",
        RememberOptions{Ttl: bound.Of(time.Second)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "v", nil
        },
    )
    if nil == rememberErr {
        t.Fatalf("expected the typed-nil cache to be refused")
    }
    if "cache instance is nil" != rememberErr.Error() {
        t.Fatalf("expected the nil-cache refusal, got: %v", rememberErr)
    }
}

/* a configuration that declares only its ttl keeps the stampede protection, so the leader is joined rather than raced */
func TestRemember_AConfigurationOfOnlyTheTtlKeepsStampedeProtection(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    releaseCallbackChannel := make(chan struct{})
    leaderStartedChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        if 1 == atomic.AddInt64(&callbackCalls, 1) {
            close(leaderStartedChannel)
        }
        <-releaseCallbackChannel
        return "value", nil
    }

    leaderResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(For(testRuntime(), cacheManager), "zero.value.option", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
        leaderResultChannel <- err
    }()

    <-leaderStartedChannel

    joinerResultChannel := make(chan error, 1)
    joinerValueChannel := make(chan any, 1)
    go func() {
        value, err := Remember(For(testRuntime(), cacheManager), "zero.value.option", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
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
        t.Fatalf("expected the ttl-only configuration to join the in-flight leader, callback calls: %d", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_ADeliberateProtectionOffWithZeroWaitIsNotCoalesced(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    config := RememberOptions{
        Ttl:                        bound.Of(time.Minute),
        StampedeProtectionDisabled: true,
        WaitTimeout:                bound.Of(time.Duration(0)),
    }

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)
    for index := 0; index < 2; index++ {
        go func() {
            defer waitGroup.Done()
            _, _ = Remember(For(testRuntime(), cacheManager), "protection.off.zero.wait", config, callback)
        }()
    }

    time.Sleep(50 * time.Millisecond)
    close(releaseCallbackChannel)
    waitGroup.Wait()

    if 2 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected the protection-off configuration to run both callbacks, got %d", atomic.LoadInt64(&callbackCalls))
    }
}

/* a payload the serializer cannot decode is a miss, not a failure: the callback recomputes and its Set overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever */
func TestRemember_RecomputesOverACorruptPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if setErr := backend.Set(testRuntime(), "poisoned", []byte("{not json"), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    value, rememberErr := Remember(
        For(testRuntime(), cacheManager),
        "poisoned",
        RememberOptions{Ttl: bound.UnlimitedDuration()},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "healed", nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("expected the corrupt payload to be recomputed, got: %v", rememberErr)
    }
    if "healed" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    storedValue, exists, getErr := Get[any](For(testRuntime(), cacheManager), "poisoned")
    if nil != getErr {
        t.Fatalf("expected the corrupt payload to have been overwritten, got: %v", getErr)
    }
    if false == exists || "healed" != storedValue.(string) {
        t.Fatalf("expected the healed value in the cache, got exists=%v value=%v", exists, storedValue)
    }
}

/* a typed-nil error from the callback reads as the success it means: boxed into a non-nil interface it would be memoized as the flight's failure and handed to every waiter */
func TestRemember_CallbackTypedNilErrorIsSuccess(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    value, rememberErr := Remember(
        For(testRuntime(), cacheManager),
        "typed.nil.callback",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            var typedNil *testRememberError
            return "computed", typedNil
        },
    )
    if nil != rememberErr {
        t.Fatalf("expected the typed-nil callback error to read as success, got: %v", rememberErr)
    }
    if "computed" != value.(string) {
        t.Fatalf("unexpected value: %v", value)
    }

    storedValue, exists, getErr := Get[any](For(testRuntime(), cacheManager), "typed.nil.callback")
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

func (instance testValueKindCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    return instance.inner.Get(runtimeInstance, key, target)
}

func (instance testValueKindCache) Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    return instance.inner.Set(runtimeInstance, key, value, ttl, cachecontract.SetOptions{})
}

func (instance testValueKindCache) Delete(runtimeInstance containercontract.Runtime, key string) error {
    return instance.inner.Delete(runtimeInstance, key)
}

func (instance testValueKindCache) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    return instance.inner.Has(runtimeInstance, key)
}

func (instance testValueKindCache) Clear(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Clear(runtimeInstance)
}

func (instance testValueKindCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    return instance.inner.Many(runtimeInstance, keys, target)
}

func (instance testValueKindCache) RoundTrip(value any, target any) error {
    return instance.inner.RoundTrip(value, target)
}

func (instance testValueKindCache) SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    return instance.inner.SetMultiple(runtimeInstance, items, ttl, cachecontract.SetOptions{})
}

func (instance testValueKindCache) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    return instance.inner.DeleteMultiple(runtimeInstance, keys)
}

func (instance testValueKindCache) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.inner.Increment(runtimeInstance, key, delta, options)
}

func (instance testValueKindCache) GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error) {
    return instance.inner.GetCounter(runtimeInstance, key)
}

func (instance testValueKindCache) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.inner.Decrement(runtimeInstance, key, delta, options)
}

func (instance testValueKindCache) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

/* a value-kind Cache has no address to tell two instances apart, so it gets no coalescing at all: one shared flight would hand a caller the value computed for somebody else's cache, and losing the stampede optimization is the price of never losing the answer */
func TestRemember_ValueKindCacheDoesNotCoalesce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    valueKindCache := testValueKindCache{inner: NewManager(backend, NewJsonSerializer(), ManagerConfig{})}

    releaseCallbackChannel := make(chan struct{})
    firstStartedChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        if 1 == atomic.AddInt64(&callbackCalls, 1) {
            close(firstStartedChannel)
        }
        <-releaseCallbackChannel
        return "value", nil
    }

    firstResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(For(testRuntime(), valueKindCache), "value.kind", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
        firstResultChannel <- err
    }()

    <-firstStartedChannel

    secondResultChannel := make(chan error, 1)
    go func() {
        _, err := Remember(For(testRuntime(), valueKindCache), "value.kind", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
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

func (instance *testPanickingSetCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    return instance.inner.Get(runtimeInstance, key, target)
}

func (instance *testPanickingSetCache) Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    panic("backend exploded")
}

func (instance *testPanickingSetCache) Delete(runtimeInstance containercontract.Runtime, key string) error {
    return instance.inner.Delete(runtimeInstance, key)
}

func (instance *testPanickingSetCache) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    return instance.inner.Has(runtimeInstance, key)
}

func (instance *testPanickingSetCache) Clear(runtimeInstance containercontract.Runtime) error {
    return instance.inner.Clear(runtimeInstance)
}

func (instance *testPanickingSetCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    return instance.inner.Many(runtimeInstance, keys, target)
}

func (instance *testPanickingSetCache) RoundTrip(value any, target any) error {
    return instance.inner.RoundTrip(value, target)
}

func (instance *testPanickingSetCache) SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    return instance.inner.SetMultiple(runtimeInstance, items, ttl, cachecontract.SetOptions{})
}

func (instance *testPanickingSetCache) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    return instance.inner.DeleteMultiple(runtimeInstance, keys)
}

func (instance *testPanickingSetCache) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.inner.Increment(runtimeInstance, key, delta, options)
}

func (instance *testPanickingSetCache) GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error) {
    return instance.inner.GetCounter(runtimeInstance, key)
}

func (instance *testPanickingSetCache) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return instance.inner.Decrement(runtimeInstance, key, delta, options)
}

func (instance *testPanickingSetCache) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

/* the callback's own panics are recovered inside the safe wrapper, so what the leader's recover catches is the cache side, and the fabricated error says so instead of blaming a callback that never misbehaved */
func TestRemember_CacheSidePanicIsNotBlamedOnTheCallback(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    panickingCache := &testPanickingSetCache{inner: NewManager(backend, NewJsonSerializer(), ManagerConfig{})}

    _, rememberErr := Remember(
        For(testRuntime(), panickingCache),
        "panicking.set",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "computed", nil
        },
    )
    if nil == rememberErr {
        t.Fatalf("expected the cache-side panic to surface as an error")
    }
    if "cache remember cache access panicked" != rememberErr.Error() {
        t.Fatalf("expected the cache-side panic message, got: %v", rememberErr)
    }
}

/* the scripted cache answers each Get from a queue and can be told to fail its Set, which is how the leader's own re-read — the one that runs after the caller already missed — is driven to each of its three outcomes deterministically rather than by racing two goroutines. */
type testScriptedCache struct {
    stateMutex   sync.Mutex
    getResults   []testScriptedGetResult
    getCallCount int
    setErr       error
    setCallCount int
    getContexts  []context.Context
    setContexts  []context.Context
    setTtls      []bound.Duration
}

type testScriptedGetResult struct {
    value  any
    exists bool
    err    error
}

func (instance *testScriptedCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    index := instance.getCallCount
    instance.getCallCount = instance.getCallCount + 1
    instance.getContexts = append(instance.getContexts, runtimeInstance.Context())

    if index >= len(instance.getResults) {
        return false, nil
    }

    result := instance.getResults[index]
    if nil != result.err || false == result.exists {
        return false, result.err
    }

    if assignErr := assignTestValue(target, result.value); nil != assignErr {
        return false, NewDeserializationError([]string{key}, assignErr)
    }

    return true, nil
}

func (instance *testScriptedCache) Set(runtimeInstance containercontract.Runtime, key string, value any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.setCallCount = instance.setCallCount + 1
    instance.setContexts = append(instance.setContexts, runtimeInstance.Context())
    instance.setTtls = append(instance.setTtls, ttl)

    return instance.setErr
}

func (instance *testScriptedCache) getCalls() int {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    return instance.getCallCount
}

func (instance *testScriptedCache) setCalls() int {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    return instance.setCallCount
}

func (instance *testScriptedCache) Delete(runtimeInstance containercontract.Runtime, key string) error { return nil }

func (instance *testScriptedCache) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) { return false, nil }

func (instance *testScriptedCache) Clear(runtimeInstance containercontract.Runtime) error { return nil }

func (instance *testScriptedCache) Many(runtimeInstance containercontract.Runtime, keys []string, target any) error {
    return nil
}

/* the scripted cache holds values, not payloads, so its copy is the value itself */
func (instance *testScriptedCache) RoundTrip(value any, target any) error {
    return assignTestValue(target, value)
}

func (instance *testScriptedCache) SetMultiple(runtimeInstance containercontract.Runtime, items map[string]any, ttl bound.Duration, _ cachecontract.SetOptions) error {
    return nil
}

func (instance *testScriptedCache) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error { return nil }

func (instance *testScriptedCache) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *testScriptedCache) GetCounter(runtimeInstance containercontract.Runtime, key string) (int64, bool, error) {
    return 0, false, nil
}

func (instance *testScriptedCache) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    return 0, nil
}

func (instance *testScriptedCache) CloseWithContext(closeContext context.Context) error { return nil }

/* a cache failure that is NOT a corrupt payload ends Remember there. The healing branch beside it — a payload the serializer cannot decode is a miss and the callback recomputes over it — was pinned; this one, the ordinary "the cache is down" answer, was not, so a Remember that swallowed a dead backend and recomputed on every single request would have looked exactly like a cache that never hits. */
func TestRemember_ACacheFailureThatIsNotACorruptPayloadEndsThere(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    callbackCalled := false

    value, rememberErr := Remember(
        For(testRuntime(), manager),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
    )

    if nil == rememberErr {
        t.Fatalf("expected the closed backend to end Remember, got %v", value)
    }

    if "cache backend is closed" != rememberErr.Error() {
        t.Fatalf("unexpected error: %v", rememberErr)
    }

    if true == callbackCalled {
        t.Fatalf("expected the callback not to run over a failed cache read")
    }

    /* with stampede protection on, the leader carries the same refusal one level down, so disarming this one changes nothing observable. Without protection there is no leader at all: this half is what proves the guard on its own position rather than on its sibling's. */
    callbackCalled = false

    value, rememberErr = Remember(
        For(testRuntime(), manager),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
    )

    if nil == rememberErr {
        t.Fatalf("expected the closed backend to end Remember without stampede protection too, got %v", value)
    }

    if "cache backend is closed" != rememberErr.Error() {
        t.Fatalf("unexpected error without stampede protection: %v", rememberErr)
    }

    if true == callbackCalled {
        t.Fatalf("expected the callback not to run over a failed cache read without stampede protection")
    }
}

/* the leader re-reads the key before computing, and a value that appears meanwhile is served instead of recomputed — that re-read is the whole point of the single flight. The scripted cache makes the caller miss and the leader hit, which is the real interleaving: another process writes the key between the two reads. */
func TestRemember_TheLeaderServesAValueThatAppearedBetweenTheTwoReads(t *testing.T) {
    scriptedCache := &testScriptedCache{
        getResults: []testScriptedGetResult{
            {value: nil, exists: false, err: nil},
            {value: "written by somebody else", exists: true, err: nil},
        },
    }

    callbackCalled := false

    value, rememberErr := Remember(
        For(testRuntime(), scriptedCache),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
    )

    if nil != rememberErr {
        t.Fatalf("unexpected error: %v", rememberErr)
    }

    if "written by somebody else" != value {
        t.Fatalf("expected the value found on the re-read, got %v", value)
    }

    if true == callbackCalled {
        t.Fatalf("expected the callback not to run once the re-read found a value")
    }

    if 0 != scriptedCache.setCalls() {
        t.Fatalf("expected nothing to be written over the value that was already there")
    }
}

/* a cache failure on the leader's re-read reaches every waiter as that failure, not as a recomputation: the leader completes the flight with the error, so the caller learns the cache is down instead of silently paying the callback on every request while believing it is cached. */
func TestRemember_ACacheFailureOnTheLeaderReReadCompletesTheFlightWithIt(t *testing.T) {
    scriptedCache := &testScriptedCache{
        getResults: []testScriptedGetResult{
            {value: nil, exists: false, err: nil},
            {value: nil, exists: false, err: errors.New("backend unreachable")},
        },
    }

    callbackCalled := false

    _, rememberErr := Remember(
        For(testRuntime(), scriptedCache),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
    )

    if nil == rememberErr {
        t.Fatalf("expected the re-read failure to end the flight")
    }

    if "backend unreachable" != rememberErr.Error() {
        t.Fatalf("unexpected error: %v", rememberErr)
    }

    if true == callbackCalled {
        t.Fatalf("expected the callback not to run over a failed re-read")
    }
}

/* a write that fails after a successful computation is reported rather than swallowed. The value IS correct — the callback produced it — so a leader that answered it anyway would look right on this request and recompute on every following one, with the cache silently never filling; the caller has to learn the write failed. */
func TestRemember_AFailedWriteIsReportedRatherThanSwallowed(t *testing.T) {
    scriptedCache := &testScriptedCache{
        getResults: []testScriptedGetResult{
            {value: nil, exists: false, err: nil},
            {value: nil, exists: false, err: nil},
        },
        setErr: errors.New("disk full"),
    }

    _, rememberErr := Remember(
        For(testRuntime(), scriptedCache),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "computed", nil
        },
    )

    if nil == rememberErr {
        t.Fatalf("expected the failed write to be reported")
    }

    if "disk full" != rememberErr.Error() {
        t.Fatalf("unexpected error: %v", rememberErr)
    }
}

/* with stampede protection deliberately off there is no leader and no flight, so both of its error exits belong to the direct path and neither was entered: a callback that failed and a write that failed both have to reach the caller, or the protection-off setting would silently become "always recompute, never report". */
func TestRemember_WithoutStampedeProtectionBothFailuresReachTheCaller(t *testing.T) {
    config := RememberOptions{Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true}

    scriptedCache := &testScriptedCache{}

    _, callbackFailureErr := Remember(
        For(testRuntime(), scriptedCache),
        "key",
        config,
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return nil, errors.New("computation refused")
        },
    )

    if nil == callbackFailureErr || "computation refused" != callbackFailureErr.Error() {
        t.Fatalf("expected the callback failure to reach the caller, got %v", callbackFailureErr)
    }

    if 0 != scriptedCache.setCalls() {
        t.Fatalf("expected nothing to be written after a failed computation")
    }

    failingWriteCache := &testScriptedCache{setErr: errors.New("disk full")}

    _, writeFailureErr := Remember(
        For(testRuntime(), failingWriteCache),
        "key",
        config,
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "computed", nil
        },
    )

    if nil == writeFailureErr || "disk full" != writeFailureErr.Error() {
        t.Fatalf("expected the write failure to reach the caller, got %v", writeFailureErr)
    }
}

/* a callback that panics is turned into an error naming the callback, on BOTH paths — the flight's and the direct one — and the two messages differ on purpose: the leader's own recover blames the cache side, so a callback panic that fell through to it would send the reader looking at the backend. Without stampede protection there is no leader to catch it at all, and the panic would leave the caller's own goroutine. */
func TestRemember_ACallbackPanicIsReportedAsTheCallbacksOnBothPaths(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        config RememberOptions
    }{
        {name: "with stampede protection", config: RememberOptions{Ttl: bound.Of(time.Minute)}},
        {name: "without stampede protection", config: RememberOptions{Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true}},
    } {
        scriptedCache := &testScriptedCache{}

        value, rememberErr := Remember(
            For(testRuntime(), scriptedCache),
            "key",
            testCase.config,
            func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
                panic("callback exploded")
            },
        )

        if nil == rememberErr {
            t.Fatalf("%s: expected the callback panic to surface as an error", testCase.name)
        }

        if "cache remember callback panicked" != rememberErr.Error() {
            t.Fatalf("%s: expected the callback to be blamed, got %v", testCase.name, rememberErr)
        }

        if nil != value {
            t.Fatalf("%s: expected no value from a panicking callback, got %v", testCase.name, value)
        }

        if 0 != scriptedCache.setCalls() {
            t.Fatalf("%s: expected nothing to be written after a panicking callback", testCase.name)
        }
    }
}

/* the last waiter of a cancelable flight cancels it, and the guard above that decision covers a call declared cancelable whose cancel function is absent. It is unreachable through Remember — the constructor builds the pair together — and stays as defense for a call assembled by hand: without it the last waiter leaving would dereference a nil function, inside a deferred call on the request path where the panic has no owner. */
func TestRememberInFlightCall_RemoveWaiterToleratesAnAbsentCancelFunction(t *testing.T) {
    shard := getRememberInFlightShard(rememberFlightKey{key: "absent.cancel.function"})

    call := newRememberInFlightCall(true)
    call.cancelFunc = nil
    call.AddWaiter()

    call.RemoveWaiter(shard)

    if true == call.IsCanceled() {
        t.Fatalf("expected a call with no cancel function to stay uncanceled")
    }
}

func TestRemember_AnswersOneShapeOnMissAndHit(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
    defer func() { _ = backend.CloseWithContext(context.Background()) }()

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        return 5, nil
    }

    missValue, missErr := Remember(For(testRuntime(), manager), "remember:shape", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
    if nil != missErr {
        t.Fatalf("miss failed: %v", missErr)
    }

    hitValue, hitErr := Remember(For(testRuntime(), manager), "remember:shape", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
    if nil != hitErr {
        t.Fatalf("hit failed: %v", hitErr)
    }

    if reflect.TypeOf(missValue) != reflect.TypeOf(hitValue) {
        t.Fatalf("expected one shape for one key, got %v on the miss and %v on the hit", reflect.TypeOf(missValue), reflect.TypeOf(hitValue))
    }

    if float64(5) != missValue.(float64) {
        t.Fatalf("expected the computing call to answer the stored shape, got %#v", missValue)
    }
}

func TestRemember_StampedeProtectedMissAnswersTheStoredShape(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
    defer func() { _ = backend.CloseWithContext(context.Background()) }()

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    value, rememberErr := Remember(
        For(testRuntime(), manager),
        "remember:stampede-shape",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return map[string]int{"a": 1}, nil
        },
    )
    if nil != rememberErr {
        t.Fatalf("remember failed: %v", rememberErr)
    }

    if _, isGenericMap := value.(map[string]any); false == isGenericMap {
        t.Fatalf("expected the protected miss to answer the stored generic shape, got %T", value)
    }
}

/* the recovery boundary keeps what the operator needs to act: the panic value travels as the cause so errors.Is still reaches the connection that refused, and the stack is captured on the goroutine that raised it. Stringified into the context alone, the framework's own idiom — a MustGet on a mistyped parameter — reached the record as a message and a cache key, with no file and no line anywhere. */
func TestExecuteRememberCallbackSafely_APanickingCallbackKeepsItsCauseAndItsStack(t *testing.T) {
    rootCause := errors.New("dial tcp 10.0.0.7:5432: connect: connection refused")

    panicValue := exception.NewError(
        "config parameter is not defined",
        map[string]any{"parameterName": "app.report.recipient"},
        rootCause,
    )

    _, _, callbackErr := executeRememberCallbackSafely(
        testRuntime(),
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            panic(panicValue)
        },
        "report:daily",
    )

    if nil == callbackErr {
        t.Fatal("expected the panic to surface as an error")
    }

    if false == errors.Is(callbackErr, rootCause) {
        t.Fatalf("expected the cause chain of the panicking error to survive, got %v", callbackErr)
    }

    typedErr, isTyped := callbackErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected a melody error, got %T", callbackErr)
    }

    stack, hasStack := typedErr.Context()["panicStack"]
    if false == hasStack {
        t.Fatalf("expected the recovery boundary to capture a stack, got context %v", typedErr.Context())
    }

    stackText, isString := stack.(string)
    if false == isString || false == strings.Contains(stackText, "executeRememberCallbackSafely") {
        t.Fatalf("expected the stack of the goroutine that raised the panic, got %v", stack)
    }
}

/* a typed-nil panic value must not reach the cause slot: its Error() dereferences a nil receiver, and the first render of the record would detonate inside the boundary that exists to write it. */
func TestExecuteRememberCallbackSafely_ATypedNilPanicValueIsNotHandedOnAsACause(t *testing.T) {
    var typedNil *exception.Error

    _, _, callbackErr := executeRememberCallbackSafely(
        testRuntime(),
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            panic(typedNil)
        },
        "report:daily",
    )

    if nil == callbackErr {
        t.Fatal("expected the panic to surface as an error")
    }

    typedErr, isTyped := callbackErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected a melody error, got %T", callbackErr)
    }

    if nil != typedErr.CauseErr() {
        t.Fatalf("expected a typed nil to answer no cause, got %v", typedErr.CauseErr())
    }

    /* the message renders, which is what a cause holding the typed nil would have taken away */
    if false == strings.Contains(callbackErr.Error(), "cache remember callback panicked") {
        t.Fatalf("unexpected message: %q", callbackErr.Error())
    }
}

/* the cache-side boundary carries the same discipline as its callback twin: a Get or Set that panicked hands its cause and its stack on rather than a flattened message. */
func TestExecuteRememberInFlightLeader_APanickingCacheAccessKeepsItsCauseAndItsStack(t *testing.T) {
    rootCause := errors.New("redis: client is closed")

    flightKey := rememberFlightKey{key: "panicking-key"}
    shard := getRememberInFlightShard(flightKey)
    call := newRememberInFlightCall(false)
    call.AddWaiter()

    shard.mutex.Lock()
    shard.inFlightByKey[flightKey] = call
    shard.mutex.Unlock()

    go executeRememberInFlightLeader(
        shard,
        flightKey,
        "report:daily",
        bound.Of(time.Minute),
        call,
        testRuntime(),
        func() {},
        nil,
        testRememberOperations(&panickingRememberCache{panicValue: exception.NewError("cache get exploded", nil, rootCause)}, "report:daily", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "value", nil
        }),
    )

    _, waitErr := call.Wait(testRuntime(), 5*time.Second, "report:daily")
    if nil == waitErr {
        t.Fatal("expected the cache-side panic to surface as an error")
    }

    if false == errors.Is(waitErr, rootCause) {
        t.Fatalf("expected the cause chain of the panicking cache to survive, got %v", waitErr)
    }

    typedErr, isTyped := waitErr.(*exception.Error)
    if false == isTyped {
        t.Fatalf("expected a melody error, got %T", waitErr)
    }

    stack, hasStack := typedErr.Context()["panicStack"]
    if false == hasStack {
        t.Fatalf("expected the cache-side boundary to capture a stack, got context %v", typedErr.Context())
    }

    /* the key alone proves nothing — an empty string under it is the same blindness with a heading */
    stackText, isString := stack.(string)
    if false == isString || false == strings.Contains(stackText, "executeRememberInFlightLeader") {
        t.Fatalf("expected the stack of the goroutine that raised the panic, got %v", stack)
    }
}

/* only Get is reached before the boundary under test, so the rest of the contract is embedded rather than written out */
type panickingRememberCache struct {
    cachecontract.Cache
    panicValue error
}

func (instance *panickingRememberCache) Get(runtimeInstance containercontract.Runtime, key string, target any) (bool, error) {
    panic(instance.panicValue)
}

func TestRemember_ACanceledCallerContextEndsThatWaitAndLeavesTheFlightForTheOthers(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(100),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    releaseCallbackChannel := make(chan struct{})
    callbackEnteredChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        close(callbackEnteredChannel)
        <-releaseCallbackChannel
        return "value", nil
    }

    leaderErrorChannel := make(chan error, 1)
    go func() {
        _, err := Remember(
            For(testRuntime(), cacheManager),
            "report.daily",
            RememberOptions{Ttl: bound.Of(time.Minute)},
            callback,
        )
        leaderErrorChannel <- err
    }()

    <-callbackEnteredChannel

    abandonedContext, cancelAbandoned := context.WithCancel(context.Background())

    abandonedErrorChannel := make(chan error, 1)
    go func() {
        _, err := Remember(
            For(testRuntimeUnder(abandonedContext), cacheManager),
            "report.daily",
            RememberOptions{Ttl: bound.Of(time.Minute)},
            callback,
        )
        abandonedErrorChannel <- err
    }()

    /* the abandoned caller has to be parked on the flight before its context goes, or the cancellation would prove nothing about the wait */
    time.Sleep(50 * time.Millisecond)

    cancelAbandoned()

    select {
    case abandonedErr := <-abandonedErrorChannel:
        if nil == abandonedErr {
            t.Fatal("expected the abandoned caller to end with an error")
        }
        if false == errors.Is(abandonedErr, context.Canceled) {
            t.Fatalf("expected context.Canceled as the cause, got %v", abandonedErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the abandoned caller stayed parked after its context was canceled")
    }

    select {
    case leaderErr := <-leaderErrorChannel:
        t.Fatalf("the leader answered while its callback was still running: %v", leaderErr)
    default:
    }

    close(releaseCallbackChannel)

    select {
    case leaderErr := <-leaderErrorChannel:
        if nil != leaderErr {
            t.Fatalf("the leader was poisoned by the caller that left: %v", leaderErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the leader never answered")
    }

    if 1 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected the callback to run once for the whole flight, ran %d times", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_WithoutStampedeProtectionTheCallbackRunsUnderTheCallerContext(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    callerContext, cancelCaller := context.WithCancel(context.Background())

    var observedContextError error

    _, rememberErr := Remember(
        For(testRuntimeUnder(callerContext), cacheManager),
        "report.uncoalesced",
        RememberOptions{Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            cancelCaller()
            observedContextError = runtimeInstance.Context().Err()
            return "value", nil
        },
    )

    if false == errors.Is(observedContextError, context.Canceled) {
        t.Fatalf("expected the uncoalesced callback to run under the caller context, saw %v", observedContextError)
    }

    if nil != rememberErr {
        t.Fatalf("expected the computed value answered, got %v", rememberErr)
    }

    if _, exists, getErr := Get[any](For(testRuntime(), cacheManager), "report.uncoalesced"); nil != getErr || false == exists {
        t.Fatalf("expected the computed value stored though the caller left, got %v, %v", exists, getErr)
    }
}

func deeplyNestedValue(depth int) any {
    var value any = "leaf"

    for level := 0; level < depth; level = level + 1 {
        value = map[string]any{"nested": value}
    }

    return value
}

/* the round-trip that makes one shape is also where a value the serializer encodes but cannot decode is found out — the JSON serializer has no depth ceiling on the way in and one on the way out; stored first, such a value was read back as a miss on every later call, recomputed, rewritten and refused again, so the refusal has to come before the store on both paths */
func TestRemember_AValueTheSerializerCannotReadBackIsNotStored(t *testing.T) {
    for _, config := range []RememberOptions{
        {Ttl: bound.Of(time.Minute)},
        {Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
    } {
        backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
        manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

        calls := 0
        callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            calls = calls + 1

            return deeplyNestedValue(20000), nil
        }

        _, rememberErr := Remember(For(testRuntime(), manager), "remember:unreadable", config, callback)
        if nil == rememberErr {
            t.Fatalf("expected the value the serializer cannot read back to be refused (protection disabled %v)", config.StampedeProtectionDisabled)
        }

        if _, exists, _ := backend.Get(testRuntime(), "remember:unreadable"); true == exists {
            t.Fatalf("expected the refused value to stay out of the backend (protection disabled %v)", config.StampedeProtectionDisabled)
        }

        if 1 != calls {
            t.Fatalf("expected the callback to run once, ran %d times (protection disabled %v)", calls, config.StampedeProtectionDisabled)
        }

        _ = backend.CloseWithContext(t.Context())
    }
}

/* rememberWithABoundedWaitOnASlowCallback runs three calls under the configuration; cancelable only narrows the window the absence is read over */
func rememberWithABoundedWaitOnASlowCallback(t *testing.T, options RememberOptions, cancelable bool) (bool, int) {
    t.Helper()

    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
    defer func() { _ = backend.CloseWithContext(context.Background()) }()

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    /* the callback ends on the release, not on a clock, so no caller's timeout can lose a race with the flight's end */
    release := make(chan struct{})

    var calls atomic.Int64
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        calls.Add(1)

        select {
        case <-runtimeInstance.Context().Done():
            return nil, runtimeInstance.Context().Err()
        case <-release:
            return "value", nil
        }
    }

    for call := 0; call < 3; call = call + 1 {
        if _, rememberErr := Remember(For(testRuntime(), manager), "remember:bounded-wait", options, callback); nil == rememberErr {
            t.Fatalf("expected call %d to time out on the slow callback", call+1)
        }

        time.Sleep(20 * time.Millisecond)
    }

    close(release)

    /* a detached flight stores once released; a cancelled one never does, and an uncancelled one would store at once, so the absence is read over a shorter window */
    waitFor := 2 * time.Second
    if true == cancelable {
        waitFor = 250 * time.Millisecond
    }

    deadline := time.Now().Add(waitFor)
    for {
        _, exists, _ := backend.Get(testRuntime(), "remember:bounded-wait")
        if true == exists || false == time.Now().Before(deadline) {
            return exists, int(calls.Load())
        }

        time.Sleep(5 * time.Millisecond)
    }
}

/* pins the consequence of a cancelable flight rather than a repair: the lone waiter's timeout cancels the flight before it can store, and every call leads a fresh flight to the same end — the key is never populated; the sibling below pins the detached pairing the documentation names */
func TestRemember_AWaitTimeoutShorterThanTheCallbackUnderTheCancelableDefaultNeverStores(t *testing.T) {
    stored, calls := rememberWithABoundedWaitOnASlowCallback(
        t,
        RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(30 * time.Millisecond)},
        true,
    )

    if true == stored {
        t.Fatalf("expected the cancelable default to store nothing when every wait times out first")
    }

    if 3 != calls {
        t.Fatalf("expected a fresh flight per call, the callback ran %d times", calls)
    }
}

func TestRemember_AWaitTimeoutShorterThanTheCallbackDetachedStoresForTheCallersAfter(t *testing.T) {
    stored, calls := rememberWithABoundedWaitOnASlowCallback(
        t,
        RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(30 * time.Millisecond), Detached: true},
        false,
    )

    if false == stored {
        t.Fatalf("expected the detached flight to store the value after the waiters left")
    }

    if 1 != calls {
        t.Fatalf("expected the later calls to coalesce on the one flight, the callback ran %d times", calls)
    }
}

/* the round-trip runs before the store, so a value the serializer cannot ENCODE is refused by the normalizer rather than by the store: the refusal keeps naming the key and the operation, as the store's own serialization refusal did, instead of answering the bare serializer error */
func TestRemember_AValueTheSerializerCannotEncodeIsRefusedNamingTheKey(t *testing.T) {
    for _, config := range []RememberOptions{
        {Ttl: bound.Of(time.Minute)},
        {Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true},
    } {
        backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
        manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

        _, rememberErr := Remember(For(testRuntime(), manager), "remember:unencodable", config, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return math.NaN(), nil
        })
        if nil == rememberErr || "cache value serialization failed" != rememberErr.Error() {
            t.Fatalf("expected the unencodable value to be refused under the framework's message, got %v (protection disabled %v)", rememberErr, config.StampedeProtectionDisabled)
        }

        if "remember:unencodable" != exception.LogContext(rememberErr, exception.LogContextOptions{})["key"] {
            t.Fatalf("expected the refusal to name the key, got %v (protection disabled %v)", exception.LogContext(rememberErr, exception.LogContextOptions{}), config.StampedeProtectionDisabled)
        }

        if _, exists, _ := backend.Get(testRuntime(), "remember:unencodable"); true == exists {
            t.Fatalf("expected the refused value to stay out of the backend (protection disabled %v)", config.StampedeProtectionDisabled)
        }

        _ = backend.CloseWithContext(t.Context())
    }
}

func TestRemember_ANilOrTypedNilOrContextlessRuntimeIsRefused(t *testing.T) {
    for _, refusedRuntime := range refusedTestRuntimes() {
        scriptedCache := &testScriptedCache{}

        _, rememberErr := Remember(For(refusedRuntime, scriptedCache), "key", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "value", nil
        })
        if nil == rememberErr || refusedRuntimeMessage != rememberErr.Error() {
            t.Fatalf("expected the absent runtime to be refused, got %v", rememberErr)
        }

        if 0 != scriptedCache.getCalls() {
            t.Fatalf("expected the cache not to be read under a refused context")
        }
    }
}

func TestRemember_EveryCallerOfOneFlightOwnsItsAnswer(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    releaseCallbackChannel := make(chan struct{})
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        <-releaseCallbackChannel
        return map[string]any{"owner": "none"}, nil
    }

    flightKey, _ := rememberFlightKeyOf(cacheManager, "report.shared", reflect.TypeFor[any](), true)
    shard := getRememberInFlightShard(flightKey)
    waiters := func() int64 {
        shard.mutex.Lock()
        defer shard.mutex.Unlock()

        call, exists := shard.inFlightByKey[flightKey]
        if false == exists {
            return 0
        }

        return call.waitersCount.Load()
    }

    callers := 3
    answers := make(chan map[string]any, callers)
    for index := 0; index < callers; index++ {
        go func() {
            value, _ := Remember(For(testRuntime(), cacheManager), "report.shared", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
            answer, _ := value.(map[string]any)
            answers <- answer
        }()
    }

    deadline := time.Now().Add(2 * time.Second)
    for int64(callers) != waiters() {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d callers parked on one flight, got %d", callers, waiters())
        }
        time.Sleep(time.Millisecond)
    }

    close(releaseCallbackChannel)

    received := make([]map[string]any, 0, callers)
    for index := 0; index < callers; index++ {
        answer := <-answers
        if nil == answer {
            t.Fatalf("expected every caller answered the map")
        }
        received = append(received, answer)
    }

    for index, answer := range received {
        answer["owner"] = index
    }

    for index, answer := range received {
        if index != answer["owner"] {
            t.Fatalf("expected caller %d to own its answer, another caller's write reached it: %v", index, answer["owner"])
        }
    }
}

/* the callback keeps the map it returned and writes it once the flight has answered its first caller, which is what a callback memoizing its own result does; every other caller answers the value as it was computed, whatever the callback writes after. Run under the race detector, a waiter copying the callback's own map is also a data race. */
func TestRemember_AWaiterNeverReadsTheValueTheCallbackKept(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(100), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    var kept map[string]any
    releaseCallbackChannel := make(chan struct{})
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        <-releaseCallbackChannel
        kept = map[string]any{"owner": "none"}
        return kept, nil
    }

    flightKey, _ := rememberFlightKeyOf(cacheManager, "report.kept", reflect.TypeFor[any](), true)
    shard := getRememberInFlightShard(flightKey)
    waiters := func() int64 {
        shard.mutex.Lock()
        defer shard.mutex.Unlock()

        call, exists := shard.inFlightByKey[flightKey]
        if false == exists {
            return 0
        }

        return call.waitersCount.Load()
    }

    callers := 32
    answers := make(chan map[string]any, callers)
    for index := 0; index < callers; index++ {
        go func() {
            value, _ := Remember(For(testRuntime(), cacheManager), "report.kept", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
            answer, _ := value.(map[string]any)
            answers <- answer
        }()
    }

    deadline := time.Now().Add(2 * time.Second)
    for int64(callers) != waiters() {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d callers parked on one flight, got %d", callers, waiters())
        }
        time.Sleep(time.Millisecond)
    }

    close(releaseCallbackChannel)

    received := []map[string]any{<-answers}

    for index := 0; index < 1000; index++ {
        kept["owner"] = "callback"
        kept["write"] = index
    }

    for index := 1; index < callers; index++ {
        received = append(received, <-answers)
    }

    for index, answer := range received {
        if nil == answer {
            t.Fatalf("expected caller %d answered the map", index)
        }

        if "none" != answer["owner"] || 1 != len(answer) {
            t.Fatalf("expected caller %d to answer the value as computed, the callback's later write reached it: %v", index, answer)
        }
    }
}

type refusingNormalizerScriptedCache struct {
    *testScriptedCache
}

func (instance *refusingNormalizerScriptedCache) RoundTrip(value any, target any) error {
    return errors.New("the stored shape cannot be read back")
}

/* a value the leader finds stored but cannot copy is answered as a hit outside a flight answers it: the first caller takes the value read, and every other caller reads its own, so no two callers share it and the stored bytes do not answer differently because callers coalesced */
func TestRemember_AStoredValueTheLeaderCannotCopyIsAnsweredAsAPlainHit(t *testing.T) {
    scriptedCache := &refusingNormalizerScriptedCache{
        testScriptedCache: &testScriptedCache{
            getResults: []testScriptedGetResult{
                {value: nil, exists: false, err: nil},
                {value: map[string]any{"owner": "none"}, exists: true, err: nil},
            },
        },
    }

    value, rememberErr := Remember(
        For(testRuntime(), scriptedCache),
        "key",
        RememberOptions{Ttl: bound.Of(time.Minute)},
        func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return "computed", nil
        },
    )

    stored, isMap := value.(map[string]any)
    if nil != rememberErr || false == isMap || "none" != stored["owner"] {
        t.Fatalf("expected the stored value answered as a hit answers it, got %v (%v)", value, rememberErr)
    }
}

/* the callers after the first each read their own stored value through the cache, so a value the leader could not copy reaches no two of them as one map */
func TestRemember_EveryWaiterReadsItsOwnValueTheLeaderCannotCopy(t *testing.T) {
    first := map[string]any{"owner": "first"}
    second := map[string]any{"owner": "second"}
    scriptedCache := &refusingNormalizerScriptedCache{
        testScriptedCache: &testScriptedCache{
            getResults: []testScriptedGetResult{
                {value: first, exists: true, err: nil},
                {value: second, exists: true, err: nil},
                {value: nil, exists: false, err: nil},
            },
        },
    }

    computations := 0
    call := newRememberInFlightCall(false)
    executeRememberInFlightLeader(&rememberInFlightShard{inFlightByKey: map[rememberFlightKey]*rememberInFlightCall{}}, rememberFlightKey{key: "key"}, "key", bound.Of(time.Minute), call, testRuntime(), func() {}, nil, testRememberOperations(scriptedCache, "key", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        computations++

        return "computed", nil
    }))

    firstBoxed, firstErr := call.answer(testRuntime())
    secondBoxed, secondErr := call.answer(testRuntime())
    firstAnswer, secondAnswer := unboxTestAnswer(firstBoxed), unboxTestAnswer(secondBoxed)
    if nil != firstErr || nil != secondErr {
        t.Fatalf("expected both callers answered, got %v and %v", firstErr, secondErr)
    }

    if "first" != firstAnswer.(map[string]any)["owner"] || "second" != secondAnswer.(map[string]any)["owner"] {
        t.Fatalf("expected the first caller the leader's read and the second its own, got %v and %v", firstAnswer, secondAnswer)
    }

    /* the entry gone before the third caller read it is a miss: the caller computes as a lone miss would, and this cache refuses to store any value, the computed one included */
    if _, goneErr := call.answer(testRuntime()); nil == goneErr || 1 != computations || false == strings.Contains(goneErr.Error(), "cache value serialization failed") {
        t.Fatalf("expected the waiter to compute over the vanished entry and the store's refusal answered, got %v after %d computations", goneErr, computations)
    }
}

/* mapRefusingScriptedCache cannot copy a stored map and copies anything else unchanged */
type mapRefusingScriptedCache struct {
    *testScriptedCache
}

func (instance *mapRefusingScriptedCache) RoundTrip(value any, target any) error {
    if _, isMap := value.(map[string]any); true == isMap {
        return errors.New("the stored shape cannot be read back")
    }

    return assignTestValue(target, value)
}

func TestRemember_AWaiterComputesWhenTheUncopyableEntryVanishedBeforeItsRead(t *testing.T) {
    scriptedCache := &mapRefusingScriptedCache{
        testScriptedCache: &testScriptedCache{
            getResults: []testScriptedGetResult{
                {value: map[string]any{"owner": "leader"}, exists: true, err: nil},
                {value: nil, exists: false, err: nil},
            },
        },
    }

    computations := 0
    call := newRememberInFlightCall(false)
    executeRememberInFlightLeader(&rememberInFlightShard{inFlightByKey: map[rememberFlightKey]*rememberInFlightCall{}}, rememberFlightKey{key: "key"}, "key", bound.Of(time.Minute), call, testRuntime(), func() {}, nil, testRememberOperations(scriptedCache, "key", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        computations++

        return "computed", nil
    }))

    if _, firstErr := call.answer(testRuntime()); nil != firstErr {
        t.Fatalf("expected the first caller answered the leader's read, got %v", firstErr)
    }

    secondBoxed, secondErr := call.answer(testRuntime())
    secondAnswer := unboxTestAnswer(secondBoxed)
    if nil != secondErr || "computed" != secondAnswer || 1 != computations || 1 != scriptedCache.setCallCount {
        t.Fatalf("expected the waiter to compute and store over the vanished entry, got %v (%v) after %d computations and %d stores", secondAnswer, secondErr, computations, scriptedCache.setCallCount)
    }
}

func TestRemember_AnAbsentTtlOrOneBelowTheMinimumIsRefusedBeforeTheCacheIsRead(t *testing.T) {
    for _, refusedTtl := range []bound.Duration{{}, bound.Of(time.Second - time.Nanosecond)} {
        scriptedCache := &testScriptedCache{}
        callbackCalled := false

        _, rememberErr := Remember(For(testRuntime(), scriptedCache), "key", RememberOptions{Ttl: refusedTtl}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            callbackCalled = true

            return "value", nil
        })
        if nil == rememberErr || "RememberOptions.Ttl" != exception.LogContext(rememberErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the ttl %s refused naming the field, got %v", refusedTtl, rememberErr)
        }

        if 0 != scriptedCache.getCalls() || true == callbackCalled {
            t.Fatalf("expected the cache not to be read and the callback not to run under a refused ttl")
        }
    }
}

func TestRemember_ANegativeWaitTimeoutIsRefusedNamingTheField(t *testing.T) {
    scriptedCache := &testScriptedCache{}

    _, rememberErr := Remember(For(testRuntime(), scriptedCache), "key", RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(-time.Second)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        return "value", nil
    })
    if nil == rememberErr || "RememberOptions.WaitTimeout" != exception.LogContext(rememberErr, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected the negative wait refused naming the field, got %v", rememberErr)
    }

    if 0 != scriptedCache.getCalls() {
        t.Fatalf("expected the cache not to be read under a refused wait")
    }
}

func TestRemember_TheWaitTimeoutRuleIsThirtySecondsAndAdmitsZeroAndUnlimited(t *testing.T) {
    absent, absentErr := bound.Resolve(bound.Duration{}, rememberWaitTimeoutRule)
    if nil != absentErr || true == absent.Unlimited || 30*time.Second != absent.Value {
        t.Fatalf("expected an absent wait to resolve to 30s, got %+v and %v", absent, absentErr)
    }

    zero, zeroErr := bound.Resolve(bound.Of(time.Duration(0)), rememberWaitTimeoutRule)
    if nil != zeroErr || 0 != zero.Value || true == zero.Unlimited {
        t.Fatalf("expected a zero wait to be admitted as no wait, got %+v and %v", zero, zeroErr)
    }

    unlimited, unlimitedErr := bound.Resolve(bound.UnlimitedDuration(), rememberWaitTimeoutRule)
    if nil != unlimitedErr || false == unlimited.Unlimited {
        t.Fatalf("expected an unlimited wait to be admitted, got %+v and %v", unlimited, unlimitedErr)
    }
}

func TestRemember_AnUnlimitedWaitIsHandedTheLeadersValue(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    leaderStarted := make(chan struct{})
    release := make(chan struct{})
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        close(leaderStarted)
        <-release

        return "value", nil
    }

    leaderDone := make(chan error, 1)
    go func() {
        _, err := Remember(For(testRuntime(), cacheManager), "unlimited.wait", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
        leaderDone <- err
    }()

    <-leaderStarted

    joinerValue := make(chan any, 1)
    joinerErr := make(chan error, 1)
    go func() {
        value, err := Remember(For(testRuntime(), cacheManager), "unlimited.wait", RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.UnlimitedDuration()}, callback)
        joinerValue <- value
        joinerErr <- err
    }()

    time.Sleep(20 * time.Millisecond)
    close(release)

    if err := <-joinerErr; nil != err {
        t.Fatalf("expected the unlimited wait to be handed the leader's value, got %v", err)
    }

    if "value" != <-joinerValue {
        t.Fatalf("unexpected joiner value")
    }

    if err := <-leaderDone; nil != err {
        t.Fatalf("leader error: %v", err)
    }
}

type rememberContextKey struct{}

func TestRemember_TheCallerReadsUnderItsContextAndTheFlightReadsAndStoresUnderItsOwn(t *testing.T) {
    scriptedCache := &testScriptedCache{}
    callerContext := context.WithValue(context.Background(), rememberContextKey{}, "caller")

    var flightContext context.Context

    _, rememberErr := Remember(For(testRuntimeUnder(callerContext), scriptedCache), "flight.contexts", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        flightContext = runtimeInstance.Context()
        return "value", nil
    })
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    scriptedCache.stateMutex.Lock()
    defer scriptedCache.stateMutex.Unlock()

    if 2 != len(scriptedCache.getContexts) || 1 != len(scriptedCache.setContexts) {
        t.Fatalf("expected two reads and one store, got %d and %d", len(scriptedCache.getContexts), len(scriptedCache.setContexts))
    }

    if "caller" != scriptedCache.getContexts[0].Value(rememberContextKey{}) {
        t.Fatal("expected the first read to carry the caller's context values")
    }

    if nil == flightContext || callerContext == flightContext {
        t.Fatal("expected the callback to run under a flight context of its own")
    }

    if flightContext != scriptedCache.getContexts[1] {
        t.Fatal("expected the leader to read under the flight's context")
    }

    if nil != scriptedCache.setContexts[0].Done() || flightContext.Value(rememberContextKey{}) != scriptedCache.setContexts[0].Value(rememberContextKey{}) {
        t.Fatal("expected the leader to store without the flight's cancellation")
    }
}

func TestReadOwnStoredValue_ReadsUnderTheFlightContext(t *testing.T) {
    scriptedCache := &testScriptedCache{getResults: []testScriptedGetResult{{value: "stored", exists: true}}}
    flightContext := context.WithValue(context.Background(), rememberContextKey{}, "flight")

    boxed, readErr := readOwnStoredValue("own.read", bound.Of(time.Minute), testRuntimeUnder(flightContext), testRememberOperations(scriptedCache, "own.read", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        return "computed", nil
    }))
    value := unboxTestAnswer(boxed)
    if nil != readErr || "stored" != value {
        t.Fatalf("expected the stored value, got %v, %v", value, readErr)
    }

    if 1 != len(scriptedCache.getContexts) || flightContext != scriptedCache.getContexts[0] {
        t.Fatal("expected the own read under the flight's context")
    }

    t.Run("a waiter's own computation honours its own item", func(t *testing.T) {
        missingCache := &testScriptedCache{}

        boxed, readErr := readOwnStoredValue("own.compute", bound.Of(time.Minute), testRuntimeUnder(flightContext), testRememberOperations(missingCache, "own.compute", func(runtimeInstance containercontract.Runtime, item *RememberItem) (any, error) {
            item.SetTtl(bound.Of(5 * time.Second))

            return "computed", nil
        }))
        if nil != readErr || "computed" != unboxTestAnswer(boxed) {
            t.Fatalf("expected the waiter's own computation, got %v, %v", unboxTestAnswer(boxed), readErr)
        }

        if 1 != len(missingCache.setTtls) || bound.Of(5*time.Second) != missingCache.setTtls[0] {
            t.Fatalf("expected one store under the ttl the waiter's callback chose, got %v", missingCache.setTtls)
        }
    })
}

func TestRemember_ADoneCallerContextStillAnswersAHit(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})
    if setErr := cacheManager.Set(testRuntime(), "report.hit", "stored", bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("planting: %v", setErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    value, rememberErr := Remember(For(testRuntimeUnder(canceledContext), cacheManager), "report.hit", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        return "computed", nil
    })
    if nil != rememberErr || "stored" != value {
        t.Fatalf("expected the hit answered under a done context, got %v, %v", value, rememberErr)
    }
}

func TestRemember_ALeaderWhoseWaitersLeftStillStoresWhatItComputed(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    cacheManager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    _, rememberErr := Remember(For(testRuntime(), cacheManager), "report.slow", RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(20 * time.Millisecond)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        time.Sleep(200 * time.Millisecond)
        return "computed", nil
    })
    if nil == rememberErr {
        t.Fatal("expected the short wait to answer before the callback")
    }

    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        if _, exists, _ := Get[any](For(testRuntime(), cacheManager), "report.slow"); true == exists {
            return
        }

        time.Sleep(5 * time.Millisecond)
    }

    t.Fatal("expected the value a callback ignoring the cancellation computed to be stored")
}

type plainDecoratorCache struct {
    cachecontract.Cache
}

func TestRemember_APlainDecoratorKeepsEveryGuarantee(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Minute), Clock: clock.NewSystemClock()})
    defer func() { _ = backend.CloseWithContext(context.Background()) }()

    decorated := &plainDecoratorCache{Cache: NewManager(backend, NewJsonSerializer(), ManagerConfig{})}

    t.Run("every caller of one flight owns its answer", func(t *testing.T) {
        releaseCallbackChannel := make(chan struct{})
        callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            <-releaseCallbackChannel
            return map[string]any{"owner": "none"}, nil
        }

        flightKey, _ := rememberFlightKeyOf(decorated, "decorated.shared", reflect.TypeFor[any](), true)
        shard := getRememberInFlightShard(flightKey)
        waiters := func() int64 {
            shard.mutex.Lock()
            defer shard.mutex.Unlock()

            call, exists := shard.inFlightByKey[flightKey]
            if false == exists {
                return 0
            }

            return call.waitersCount.Load()
        }

        callers := 2
        answers := make(chan map[string]any, callers)
        for index := 0; index < callers; index++ {
            go func() {
                value, _ := Remember(For(testRuntime(), decorated), "decorated.shared", RememberOptions{Ttl: bound.Of(time.Minute)}, callback)
                answer, _ := value.(map[string]any)
                answers <- answer
            }()
        }

        deadline := time.Now().Add(2 * time.Second)
        for int64(callers) != waiters() {
            if true == time.Now().After(deadline) {
                t.Fatalf("expected %d callers parked on one flight, got %d", callers, waiters())
            }
            time.Sleep(time.Millisecond)
        }

        close(releaseCallbackChannel)

        first := <-answers
        second := <-answers
        if nil == first || nil == second {
            t.Fatalf("expected both callers answered the map, got %v and %v", first, second)
        }

        first["owner"] = "first"
        if "none" != second["owner"] {
            t.Fatalf("expected the second caller to own its answer, the first caller's write reached it: %v", second)
        }
    })

    t.Run("the computing call answers the stored shape", func(t *testing.T) {
        value, rememberErr := Remember(For(testRuntime(), decorated), "decorated.shape", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return 5, nil
        })
        if nil != rememberErr {
            t.Fatalf("remember failed: %v", rememberErr)
        }

        if float64(5) != value {
            t.Fatalf("expected the computing call to answer what a hit answers, float64(5), got %#v", value)
        }
    })

    t.Run("a value that cannot be read back is refused with nothing stored", func(t *testing.T) {
        _, rememberErr := Remember(For(testRuntime(), decorated), "decorated.unreadable", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
            return deeplyNestedValue(20000), nil
        })
        if nil == rememberErr {
            t.Fatalf("expected the value the cache cannot read back to be refused")
        }

        if hasValue, _ := decorated.Has(testRuntime(), "decorated.unreadable"); true == hasValue {
            t.Fatalf("expected the refused value to stay out of the store")
        }
    })
}

const rememberFlightServiceName = "remember.flight.probe"

type rememberFlightScopedProbe struct {
    closed chan struct{}
}

func (instance *rememberFlightScopedProbe) CloseWithContext(closeContext context.Context) error {
    close(instance.closed)

    return nil
}

func newRememberFlightContainer(t *testing.T, probeList chan *rememberFlightScopedProbe) containercontract.Container {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registerErr := serviceContainer.Register(
        rememberFlightServiceName,
        func(resolver containercontract.Resolver) (string, error) {
            return "resolved", nil
        },
        containercontract.RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    scopedErr := serviceContainer.RegisterScoped(
        "remember.flight.scoped",
        func(resolver containercontract.Resolver) (*rememberFlightScopedProbe, error) {
            probe := &rememberFlightScopedProbe{closed: make(chan struct{})}
            probeList <- probe

            return probe, nil
        },
        containercontract.RegisterOptions{TypeRegistrationDisabled: true},
    )
    if nil != scopedErr {
        t.Fatalf("register scoped: %v", scopedErr)
    }

    return serviceContainer
}

func TestRemember_TheFlightComputesInAScopeOfItsOwnThatOutlivesTheLeadersRequest(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    leaderScope := serviceContainer.NewScope()
    leaderRuntime := runtime.NewRequest(context.Background(), leaderScope)

    callbackEntered := make(chan struct{})
    releaseCallback := make(chan struct{})
    var callbackKind containercontract.Kind
    var callbackScope containercontract.Scope
    callback := func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        close(callbackEntered)
        <-releaseCallback

        callbackKind = runtimeInstance.Kind()
        callbackScope = runtimeInstance.Scope()

        return runtime.FromRuntime[string](runtimeInstance, rememberFlightServiceName)
    }

    config := RememberOptions{Ttl: bound.Of(time.Minute), Detached: true}
    leaderAnswer := make(chan error, 1)
    go func() {
        _, rememberErr := Remember(For(leaderRuntime, cacheManager), "flight.scope", config, callback)
        leaderAnswer <- rememberErr
    }()

    <-callbackEntered

    waiterAnswer := make(chan string, 1)
    waiterError := make(chan error, 1)
    go func() {
        value, rememberErr := Remember(For(testRuntime(), cacheManager), "flight.scope", config, callback)
        waiterAnswer <- value
        waiterError <- rememberErr
    }()

    if closeErr := leaderScope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("closing the leader's scope: %v", closeErr)
    }
    close(releaseCallback)

    if rememberErr := <-waiterError; nil != rememberErr {
        t.Fatalf("expected the waiter answered the value the callback resolved, got %v", rememberErr)
    }

    if "resolved" != <-waiterAnswer {
        t.Fatalf("expected the waiter answered the resolved value")
    }

    <-leaderAnswer

    if containercontract.KindRun != callbackKind {
        t.Fatalf("expected the flight to compute under a run, got %s", callbackKind)
    }

    if leaderScope == callbackScope {
        t.Fatalf("expected the flight's scope to be its own, not the leader's")
    }
}

func TestRemember_TheFlightClosesTheScopeItOpened(t *testing.T) {
    probeList := make(chan *rememberFlightScopedProbe, 4)
    serviceContainer := newRememberFlightContainer(t, probeList)
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    callerRuntime := runtime.NewRequest(context.Background(), serviceContainer.NewScope())
    value, rememberErr := Remember(For(callerRuntime, cacheManager), "flight.close", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        _, resolveErr := runtime.FromRuntime[*rememberFlightScopedProbe](runtimeInstance, "remember.flight.scoped")

        return "computed", resolveErr
    })
    if nil != rememberErr || "computed" != value {
        t.Fatalf("expected the computed value, got %q and %v", value, rememberErr)
    }

    probe := <-probeList
    select {
    case <-probe.closed:
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the flight's scope closed once the flight completed")
    }

    select {
    case <-callerRuntime.Context().Done():
        t.Fatalf("the caller's runtime is not the flight's to end")
    default:
    }
}

func TestRemember_WithoutAFlightTheCallbackRunsUnderTheCallersRuntime(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    callerScope := serviceContainer.NewScope()
    callerRuntime := runtime.NewRequest(context.Background(), callerScope)

    var callbackRuntime containercontract.Runtime
    _, rememberErr := Remember(For(callerRuntime, cacheManager), "flight.none", RememberOptions{Ttl: bound.Of(time.Minute), StampedeProtectionDisabled: true}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        callbackRuntime = runtimeInstance

        return "computed", nil
    })
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if callerRuntime != callbackRuntime {
        t.Fatalf("expected the caller's runtime handed to the callback")
    }
}

func TestRemember_AWaiterThatComputesResolvesThroughItsOwnScope(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    scriptedCache := &mapRefusingScriptedCache{
        testScriptedCache: &testScriptedCache{
            getResults: []testScriptedGetResult{
                {value: map[string]any{"owner": "leader"}, exists: true, err: nil},
                {value: nil, exists: false, err: nil},
            },
        },
    }

    flightScope := serviceContainer.NewScope()
    flightRuntime := runtime.NewRun(context.Background(), flightScope)

    call := newRememberInFlightCall(true)
    executeRememberInFlightLeader(&rememberInFlightShard{inFlightByKey: map[rememberFlightKey]*rememberInFlightCall{}}, rememberFlightKey{key: "key"}, "key", bound.Of(time.Minute), call, flightRuntime, func() { _ = flightScope.CloseWithContext(context.Background()) }, nil, testRememberOperations(scriptedCache, "key", func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        if contextErr := runtimeInstance.Context().Err(); nil != contextErr {
            return nil, contextErr
        }

        return runtime.FromRuntime[string](runtimeInstance, rememberFlightServiceName)
    }))

    if _, firstErr := call.answer(testRuntime()); nil != firstErr {
        t.Fatalf("expected the first caller the leader's read, got %v", firstErr)
    }

    /* the waiter's context is done, the flight's is not: the computation runs under the flight's */
    waiterContext, cancelWaiter := context.WithCancel(context.Background())
    cancelWaiter()
    waiterRuntime := runtime.NewRequest(waiterContext, serviceContainer.NewScope())
    boxed, waiterErr := call.answer(waiterRuntime)
    if nil != waiterErr {
        t.Fatalf("expected the waiter to resolve through its own scope, got %v", waiterErr)
    }

    if "resolved" != unboxTestAnswer(boxed) {
        t.Fatalf("expected the waiter's own computation, got %v", unboxTestAnswer(boxed))
    }
}

func TestRemember_AFlightLedAtBootComputesUnderTheBootsScope(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    bootRuntime, finishBoot := runtime.NewBoot(context.Background(), serviceContainer)
    defer func() { _ = finishBoot() }()

    var callbackKind containercontract.Kind
    value, rememberErr := Remember(For(bootRuntime, cacheManager), "flight.boot", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        callbackKind = runtimeInstance.Kind()

        return runtime.FromRuntime[string](runtimeInstance, rememberFlightServiceName)
    })
    if nil != rememberErr || "resolved" != value {
        t.Fatalf("expected the boot-led flight to resolve, got %q and %v", value, rememberErr)
    }

    if containercontract.KindBoot != callbackKind {
        t.Fatalf("expected the boot's kind lent to the flight, got %s", callbackKind)
    }
}

func TestRemember_AFlightLedAtBootComputesUnderTheFlightsContext(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    bootRuntime, finishBoot := runtime.NewBoot(context.Background(), serviceContainer)
    defer func() { _ = finishBoot() }()

    canceled := make(chan struct{})
    _, rememberErr := Remember(For(bootRuntime, cacheManager), "flight.boot.cancel", RememberOptions{Ttl: bound.Of(time.Minute), WaitTimeout: bound.Of(10 * time.Millisecond)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        <-runtimeInstance.Context().Done()
        close(canceled)

        return "", runtimeInstance.Context().Err()
    })
    if nil == rememberErr {
        t.Fatalf("expected the boot leader's wait to time out")
    }

    select {
    case <-canceled:
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the flight's context canceled once its last waiter left")
    }
}

func TestRemember_AViewKeptPastItsClosedScopeIsAnsweredTheClosedScopeAndStoresNothing(t *testing.T) {
    serviceContainer := newRememberFlightContainer(t, make(chan *rememberFlightScopedProbe, 4))
    cacheManager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer cacheManager.CloseWithContext(context.Background())

    keptScope := serviceContainer.NewScope()
    keptView := For(runtime.NewRequest(context.Background(), keptScope), cacheManager)
    if closeErr := keptScope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    computed := false
    _, rememberErr := Remember(keptView, "flight.kept", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (string, error) {
        computed = true

        return "computed", nil
    })
    if false == errors.Is(rememberErr, container.ErrScopeClosed) {
        t.Fatalf("expected the closed scope's error, got %v", rememberErr)
    }

    if true == computed {
        t.Fatalf("expected nothing computed under a closed scope")
    }

    if held, _ := cacheManager.Has(testRuntime(), "flight.kept"); true == held {
        t.Fatalf("expected nothing stored under a closed scope")
    }
}

/* scopelessTestRuntime carries a context and no scope, which only a runtime of another implementation can */
type scopelessTestRuntime struct {
    containercontract.Runtime
}

func (instance scopelessTestRuntime) Context() context.Context {
    return context.Background()
}

func (instance scopelessTestRuntime) Scope() containercontract.Scope {
    return nil
}

func TestRemember_ARuntimeWithoutAScopeIsRefused(t *testing.T) {
    scriptedCache := &testScriptedCache{}

    _, rememberErr := Remember(For(scopelessTestRuntime{}, scriptedCache), "key", RememberOptions{Ttl: bound.Of(time.Minute)}, func(runtimeInstance containercontract.Runtime, _ *RememberItem) (any, error) {
        return "value", nil
    })
    if nil == rememberErr || "the runtime carries no scope" != rememberErr.Error() {
        t.Fatalf("expected the scopeless runtime refused, got %v", rememberErr)
    }

    if 0 != scriptedCache.getCalls() {
        t.Fatalf("expected the cache not read under a refused runtime")
    }
}
