package cache

import (
    "context"
    "errors"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    cachecontract "github.com/precision-soft/melody/cache/contract"
    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/exception"
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

/* the guard reads through the interface: a typed-nil Cache is a non-nil interface that passed the plain comparison and panicked on the first method call, on the request path, in place of the error the refusal promises */
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

/* the zero-value option is constructible from outside the package and silently disarmed the stampede protection it never asked to configure; it reads as the constructor defaults instead, so the leader is joined rather than raced */
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

func TestRememberOption_ASetterOnTheZeroValueStartsFromTheDefaults(t *testing.T) {
    protectionOn := (&RememberOption{}).WithStampedeProtectionEnabled(true)
    if -1 != protectionOn.WaitTimeout() {
        t.Fatalf("expected the zero receiver to read as the defaults before the setter, got waitTimeout %v", protectionOn.WaitTimeout())
    }

    cancelable := (&RememberOption{}).WithCancelable(true)
    if false == cancelable.EnableStampedeProtection() || -1 != cancelable.WaitTimeout() {
        t.Fatalf("expected cancelability alone to leave the protection and the wait at their defaults, got %v/%v", cancelable.EnableStampedeProtection(), cancelable.WaitTimeout())
    }

    explicitNoWait := NewDefaultRememberOption().WithWaitTimeout(0)
    if 0 != explicitNoWait.WaitTimeout() {
        t.Fatalf("expected the constructor-built option to keep its explicit zero wait, got %v", explicitNoWait.WaitTimeout())
    }

    protectionOff := (&RememberOption{}).WithStampedeProtectionEnabled(false)
    if true == protectionOff.EnableStampedeProtection() {
        t.Fatalf("expected the deliberate protection-off setter to stay what it says")
    }
}

func TestRememberOption_ADeliberateProtectionOffWithZeroWaitStaysWhatItSays(t *testing.T) {
    option := NewDefaultRememberOption().
        WithStampedeProtectionEnabled(false).
        WithWaitTimeout(0)

    if true == option.EnableStampedeProtection() {
        t.Fatalf("expected protection to stay off when both fields land on their zero value, got it armed")
    }

    if 0 != option.WaitTimeout() {
        t.Fatalf("expected the explicit zero wait to survive, got %v", option.WaitTimeout())
    }
}

func TestRememberOption_AnUnspokenWaitReadsTheDefaultAndAnExplicitZeroReadsZero(t *testing.T) {
    unspoken := NewDefaultRememberOption().WithStampedeProtectionEnabled(false)
    if -1 != unspoken.WaitTimeout() {
        t.Fatalf("expected an unspoken wait to read the default -1, got %v", unspoken.WaitTimeout())
    }

    explicitZero := NewDefaultRememberOption().WithWaitTimeout(0)
    if 0 != explicitZero.WaitTimeout() {
        t.Fatalf("expected an explicit zero wait to read zero, got %v", explicitZero.WaitTimeout())
    }

    rawZero := &RememberOption{}
    if -1 != rawZero.WaitTimeout() {
        t.Fatalf("expected a value built outside the constructor to read the default -1, got %v", rawZero.WaitTimeout())
    }
}

func TestRemember_ADeliberateProtectionOffWithZeroWaitIsNotCoalesced(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    cacheManager := NewManager(backend, NewJsonSerializer())

    option := NewDefaultRememberOption().
        WithStampedeProtectionEnabled(false).
        WithWaitTimeout(0)

    releaseCallbackChannel := make(chan struct{})

    var callbackCalls int64
    callback := func(ctx context.Context) (any, error) {
        atomic.AddInt64(&callbackCalls, 1)
        <-releaseCallbackChannel
        return "value", nil
    }

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)
    for index := 0; index < 2; index++ {
        go func() {
            defer waitGroup.Done()
            _, _ = Remember(cacheManager, "protection.off.zero.wait", time.Minute, callback, option)
        }()
    }

    time.Sleep(50 * time.Millisecond)
    close(releaseCallbackChannel)
    waitGroup.Wait()

    if 2 != atomic.LoadInt64(&callbackCalls) {
        t.Fatalf("expected the protection-off option to run both callbacks, got %d — the two-off option collapsed to the defaults", atomic.LoadInt64(&callbackCalls))
    }
}

func TestRemember_AZeroOptionPlusASetterAnswersTheMissWithTheValue(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(100, time.Hour, clockInstance)
    defer backend.Close()

    cacheManager := NewManager(backend, NewJsonSerializer())

    value, err := Remember(
        cacheManager,
        "zero.option.plus.setter",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return "computed", nil
        },
        (&RememberOption{}).WithStampedeProtectionEnabled(true),
    )
    if nil != err {
        t.Fatalf("expected the miss to answer the computed value, got error: %v", err)
    }

    if "computed" != value {
        t.Fatalf("unexpected value: %#v", value)
    }
}

/* a payload the serializer cannot decode is a miss, not a failure: the callback recomputes and its Set overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever */
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

/* a typed-nil error from the callback reads as the success it means: boxed into a non-nil interface it was memoized as the flight's failure, handed to every waiter, and panicked the first one that rendered it */
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

/* a value-kind Cache has no address to tell two instances apart, so it gets no coalescing at all: one shared flight would hand a caller the value computed for somebody else's cache, and losing the stampede optimization is the price of never losing the answer */
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

/* the callback's own panics are recovered inside the safe wrapper, so what the leader's recover catches is the cache side, and the fabricated error says so instead of blaming a callback that never misbehaved */
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

/* the scripted cache answers each Get from a queue and can be told to fail its Set, which is how the leader's own re-read — the one that runs after the caller already missed — is driven to each of its three outcomes deterministically rather than by racing two goroutines. */
type testScriptedCache struct {
    stateMutex   sync.Mutex
    getResults   []testScriptedGetResult
    getCallCount int
    setErr       error
    setCallCount int
}

type testScriptedGetResult struct {
    value  any
    exists bool
    err    error
}

func (instance *testScriptedCache) Get(key string) (any, bool, error) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    index := instance.getCallCount
    instance.getCallCount = instance.getCallCount + 1

    if index >= len(instance.getResults) {
        return nil, false, nil
    }

    result := instance.getResults[index]

    return result.value, result.exists, result.err
}

func (instance *testScriptedCache) Set(key string, value any, ttl time.Duration) error {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.setCallCount = instance.setCallCount + 1

    return instance.setErr
}

func (instance *testScriptedCache) setCalls() int {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    return instance.setCallCount
}

func (instance *testScriptedCache) Delete(key string) error { return nil }

func (instance *testScriptedCache) Has(key string) (bool, error) { return false, nil }

func (instance *testScriptedCache) Clear() error { return nil }

func (instance *testScriptedCache) Many(keys []string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (instance *testScriptedCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *testScriptedCache) DeleteMultiple(keys []string) error { return nil }

func (instance *testScriptedCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *testScriptedCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *testScriptedCache) Close() error { return nil }

/* a cache failure that is NOT a corrupt payload ends Remember there. The healing branch beside it — a payload the serializer cannot decode is a miss and the callback recomputes over it — was pinned; this one, the ordinary "the cache is down" answer, was not, so a Remember that swallowed a dead backend and recomputed on every single request would have looked exactly like a cache that never hits. */
func TestRemember_ACacheFailureThatIsNotACorruptPayloadEndsThere(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(10, time.Hour, clockInstance)
    manager := NewManager(backend, NewJsonSerializer())

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    callbackCalled := false

    value, rememberErr := Remember(
        manager,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
        nil,
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
        manager,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
        NewDefaultRememberOption().WithStampedeProtectionEnabled(false),
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

/* the leader re-reads the key before computing, and a value that appeared meanwhile is served instead of recomputed — that re-read is the whole point of the single flight, and it had no test that made it FIND something. The scripted cache makes the caller miss and the leader hit, which is the real interleaving: another process wrote the key between the two reads. */
func TestRemember_TheLeaderServesAValueThatAppearedBetweenTheTwoReads(t *testing.T) {
    scriptedCache := &testScriptedCache{
        getResults: []testScriptedGetResult{
            {value: nil, exists: false, err: nil},
            {value: "written by somebody else", exists: true, err: nil},
        },
    }

    callbackCalled := false

    value, rememberErr := Remember(
        scriptedCache,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
        nil,
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
        scriptedCache,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            callbackCalled = true
            return "computed", nil
        },
        nil,
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
        scriptedCache,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return "computed", nil
        },
        nil,
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
    option := NewDefaultRememberOption().WithStampedeProtectionEnabled(false)

    scriptedCache := &testScriptedCache{}

    _, callbackFailureErr := Remember(
        scriptedCache,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return nil, errors.New("computation refused")
        },
        option,
    )

    if nil == callbackFailureErr || "computation refused" != callbackFailureErr.Error() {
        t.Fatalf("expected the callback failure to reach the caller, got %v", callbackFailureErr)
    }

    if 0 != scriptedCache.setCalls() {
        t.Fatalf("expected nothing to be written after a failed computation")
    }

    failingWriteCache := &testScriptedCache{setErr: errors.New("disk full")}

    _, writeFailureErr := Remember(
        failingWriteCache,
        "key",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return "computed", nil
        },
        option,
    )

    if nil == writeFailureErr || "disk full" != writeFailureErr.Error() {
        t.Fatalf("expected the write failure to reach the caller, got %v", writeFailureErr)
    }
}

/* a callback that panics is turned into an error naming the callback, on BOTH paths — the flight's and the direct one — and the two messages differ on purpose: the leader's own recover blames the cache side, so a callback panic that fell through to it would send the reader looking at the backend. Without stampede protection there is no leader to catch it at all, and the panic would leave the caller's own goroutine. */
func TestRemember_ACallbackPanicIsReportedAsTheCallbacksOnBothPaths(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        option *RememberOption
    }{
        {name: "with stampede protection", option: nil},
        {name: "without stampede protection", option: NewDefaultRememberOption().WithStampedeProtectionEnabled(false)},
    } {
        scriptedCache := &testScriptedCache{}

        value, rememberErr := Remember(
            scriptedCache,
            "key",
            time.Minute,
            func(ctx context.Context) (any, error) {
                panic("callback exploded")
            },
            testCase.option,
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
    shard := getRememberInFlightShard("absent.cancel.function")

    call := newRememberInFlightCall(true)
    call.cancelFunc = nil
    call.AddWaiter()

    call.RemoveWaiter(shard)

    if true == call.IsCanceled() {
        t.Fatalf("expected a call with no cancel function to stay uncanceled")
    }
}

func TestRemember_AnswersOneShapeOnMissAndHit(t *testing.T) {
    backend := NewInMemoryBackend(0, time.Minute, clock.NewSystemClock())
    defer func() { _ = backend.Close() }()

    manager := NewManager(backend, NewJsonSerializer())

    callback := func(ctx context.Context) (any, error) {
        return 5, nil
    }

    missValue, missErr := Remember(manager, "remember:shape", time.Minute, callback, nil)
    if nil != missErr {
        t.Fatalf("miss failed: %v", missErr)
    }

    hitValue, hitErr := Remember(manager, "remember:shape", time.Minute, callback, nil)
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
    backend := NewInMemoryBackend(0, time.Minute, clock.NewSystemClock())
    defer func() { _ = backend.Close() }()

    manager := NewManager(backend, NewJsonSerializer())

    value, rememberErr := Remember(
        manager,
        "remember:stampede-shape",
        time.Minute,
        func(ctx context.Context) (any, error) {
            return map[string]int{"a": 1}, nil
        },
        NewDefaultRememberOption(),
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

    _, callbackErr := executeRememberCallbackSafely(
        context.Background(),
        func(ctx context.Context) (any, error) {
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

    _, callbackErr := executeRememberCallbackSafely(
        context.Background(),
        func(ctx context.Context) (any, error) {
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

    shard := getRememberInFlightShard("panicking-key")
    call := newRememberInFlightCall(false)
    call.AddWaiter()

    shard.mutex.Lock()
    shard.inFlightByKey["panicking-key"] = call
    shard.mutex.Unlock()

    go executeRememberInFlightLeader(
        &panickingRememberCache{panicValue: exception.NewError("cache get exploded", nil, rootCause)},
        shard,
        "panicking-key",
        "report:daily",
        time.Minute,
        call,
        func(ctx context.Context) (any, error) {
            return "value", nil
        },
    )

    _, waitErr := call.Wait(5*time.Second, "report:daily")
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

func (instance *panickingRememberCache) Get(key string) (any, bool, error) {
    panic(instance.panicValue)
}
