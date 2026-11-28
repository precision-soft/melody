package cache

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func TestNewManager_PanicsOnNilBackendOrSerializer(t *testing.T) {
    defer func() {
        _ = recover()
    }()

    func() {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected panic on nil backend")
            }
        }()

        _ = NewManager(nil, NewJsonSerializer(), ManagerConfig{})
    }()

    func() {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected panic on nil serializer")
            }
        }()

        clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        _ = NewManager(backend, nil, ManagerConfig{})
    }()
}

func TestManager_SetGetManySetMultipleDeleteMultipleClearDeleteHasClose(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{
        MaxItems:        bound.Of(10),
        CleanupInterval: bound.Of(time.Hour),
        Clock:           clockInstance,
    })
    defer backend.CloseWithContext(context.Background())

    cacheInstance := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    setErr := cacheInstance.Set(testRuntime(), "a", "1", bound.UnlimitedDuration(), cachecontract.SetOptions{})
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    value, exists, getErr := Get[any](For(testRuntime(), cacheInstance), "a")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "1" != value.(string) {
        t.Fatalf("unexpected value")
    }

    hasValue, hasErr := cacheInstance.Has(testRuntime(), "a")
    if nil != hasErr {
        t.Fatalf("has error: %v", hasErr)
    }
    if true != hasValue {
        t.Fatalf("expected has true")
    }

    many, manyErr := Many[any](For(testRuntime(), cacheInstance), []string{"a", "b"})
    if nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }
    if "1" != many["a"].(string) {
        t.Fatalf("unexpected many[a]")
    }
    if nil != many["b"] {
        t.Fatalf("expected missing key not to exist in result map")
    }

    setMultipleErr := cacheInstance.SetMultiple(
        testRuntime(),
        map[string]any{
            "b": "2",
            "c": "3",
        },
        bound.UnlimitedDuration(),
        cachecontract.SetOptions{},
    )
    if nil != setMultipleErr {
        t.Fatalf("setMultiple error: %v", setMultipleErr)
    }

    many, manyErr = Many[any](For(testRuntime(), cacheInstance), []string{"a", "b", "c"})
    if nil != manyErr {
        t.Fatalf("many error: %v", manyErr)
    }
    if "2" != many["b"].(string) {
        t.Fatalf("unexpected many[b]")
    }
    if "3" != many["c"].(string) {
        t.Fatalf("unexpected many[c]")
    }

    deleteMultipleErr := cacheInstance.DeleteMultiple(testRuntime(), []string{"b", "c"})
    if nil != deleteMultipleErr {
        t.Fatalf("deleteMultiple error: %v", deleteMultipleErr)
    }

    _, exists, getErr = Get[any](For(testRuntime(), cacheInstance), "b")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected b deleted")
    }

    deleteErr := cacheInstance.Delete(testRuntime(), "a")
    if nil != deleteErr {
        t.Fatalf("delete error: %v", deleteErr)
    }

    _, exists, getErr = Get[any](For(testRuntime(), cacheInstance), "a")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected a deleted")
    }

    setErr = cacheInstance.Set(testRuntime(), "x", "y", bound.UnlimitedDuration(), cachecontract.SetOptions{})
    if nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    clearErr := cacheInstance.Clear(testRuntime())
    if nil != clearErr {
        t.Fatalf("clear error: %v", clearErr)
    }

    _, exists, getErr = Get[any](For(testRuntime(), cacheInstance), "x")
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    if true == exists {
        t.Fatalf("expected cache cleared")
    }

    closeErr := cacheInstance.CloseWithContext(t.Context())
    if nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }
}

func TestNewManager_DoesNotCloseTheBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if closeErr := manager.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set(testRuntime(), "a", []byte("1"), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("expected the backend to stay open after the non-owning manager closed, got: %v", setErr)
    }
}

func TestNewManager_AnOwnedBackendIsClosedWithTheManager(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{OwnsBackend: true})

    if closeErr := manager.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close error: %v", closeErr)
    }

    if setErr := backend.Set(testRuntime(), "a", []byte("1"), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil == setErr {
        t.Fatalf("expected the backend to be closed by the owning manager")
    }
}

func TestManager_Get_TypesTheDeserializationFailure(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if setErr := backend.Set(testRuntime(), "corrupt", []byte("{not json"), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    _, exists, getErr := Get[any](For(testRuntime(), manager), "corrupt")
    if nil == getErr {
        t.Fatalf("expected a deserialization error")
    }
    if true == exists {
        t.Fatalf("expected exists false on a corrupt payload")
    }
    if false == IsDeserializationError(getErr) {
        t.Fatalf("expected the error to be typed as a DeserializationError, got: %v", getErr)
    }
}

func TestManager_Many_SkipsCorruptEntriesAndNamesThem(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if setErr := manager.Set(testRuntime(), "good.one", "1", bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }
    if setErr := manager.Set(testRuntime(), "good.two", "2", bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }
    if setErr := backend.Set(testRuntime(), "corrupt", []byte("{not json"), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("set error: %v", setErr)
    }

    result, manyErr := Many[any](For(testRuntime(), manager), []string{"good.one", "corrupt", "good.two", "missing"})
    if nil == manyErr {
        t.Fatalf("expected an error naming the corrupt entry")
    }
    if false == IsDeserializationError(manyErr) {
        t.Fatalf("expected the error to be typed as a DeserializationError, got: %v", manyErr)
    }

    if 2 != len(result) {
        t.Fatalf("expected both good values beside the error, got %d", len(result))
    }
    if "1" != result["good.one"].(string) || "2" != result["good.two"].(string) {
        t.Fatalf("unexpected surviving values: %v", result)
    }

    var deserializationErr *DeserializationError
    if false == errors.As(manyErr, &deserializationErr) {
        t.Fatalf("expected errors.As to find the DeserializationError")
    }

    keys, ok := deserializationErr.exceptionErr.Context()["keys"].([]string)
    if false == ok || 1 != len(keys) || "corrupt" != keys[0] {
        t.Fatalf("expected the error context to name exactly the corrupt key, got: %v", deserializationErr.exceptionErr.Context()["keys"])
    }
}

/* the items come as a map, so a refusal naming whichever entry the iteration reached first would differ between calls; fifty rounds make a first-refusal-wins rule visible */
func TestManager_SetMultipleNamesTheRefusedKeysDeterministically(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    for round := 0; round < 50; round++ {
        setErr := manager.SetMultiple(testRuntime(), map[string]any{"b.bad": make(chan int), "a.bad": make(chan int), "good": "kept"}, bound.UnlimitedDuration(), cachecontract.SetOptions{})
        if nil == setErr {
            t.Fatalf("expected a serialization error from SetMultiple")
        }

        var exceptionErr *exception.Error
        if false == errors.As(setErr, &exceptionErr) {
            t.Fatalf("expected an exception error, got: %v", setErr)
        }

        if "a.bad" != exceptionErr.Context()["key"] {
            t.Fatalf("round %d: expected the first refused key in order, got: %v", round, exceptionErr.Context()["key"])
        }

        keys, isSlice := exceptionErr.Context()["keys"].([]string)
        if false == isSlice || 2 != len(keys) || "a.bad" != keys[0] || "b.bad" != keys[1] {
            t.Fatalf("round %d: expected every refused key in order, got: %v", round, exceptionErr.Context()["keys"])
        }
    }

    if _, exists, _ := backend.Get(testRuntime(), "good"); true == exists {
        t.Fatalf("expected a refused SetMultiple to write nothing")
    }
}

/* Serializer is a PUBLIC contract, so the refusal SetMultiple reads to build its per-key reasons is the
   application's error. Reading its text bare turned a failure this door ANSWERS into a panic nothing on
   the request path contains — the sibling Set, which hands the same error to NewError without reading it,
   answered an error for the same input on every major. The text is read the way the repository reads every
   foreign error's text, under a recover, and the marker takes the place of the reason. */
func TestManager_SetMultipleAnswersARefusalWhenTheSerializersErrorTextPanics(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, &panickingTextSerializer{}, ManagerConfig{})

    var recoveredValue any
    var setErr error

    func() {
        defer func() {
            recoveredValue = recover()
        }()

        setErr = manager.SetMultiple(testRuntime(), map[string]any{"a.bad": "first", "b.bad": "second"}, bound.UnlimitedDuration(), cachecontract.SetOptions{})
    }()

    if nil != recoveredValue {
        t.Fatalf("expected a refusal; the door panicked with the serializer's own error text: %v", recoveredValue)
    }

    if nil == setErr {
        t.Fatalf("expected a serialization error from SetMultiple")
    }

    var exceptionErr *exception.Error
    if false == errors.As(setErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setErr)
    }

    causeByKey, isMap := exceptionErr.Context()["causeByKey"].(map[string]string)
    if false == isMap || 2 != len(causeByKey) {
        t.Fatalf("expected a reason for each refused key, got: %v", exceptionErr.Context()["causeByKey"])
    }

    for _, refusedKey := range []string{"a.bad", "b.bad"} {
        if "serialization error message panicked: the serializer's error text panicked" != causeByKey[refusedKey] {
            t.Fatalf("expected %s to carry the panic in place of its reason, got: %q", refusedKey, causeByKey[refusedKey])
        }
    }
}

/* an error of the application's whose Error() panics — typically on the very field that made the
   serialization fail — is the value this whole class is about; it is not producible through any shipped
   serializer, so the double is what makes the guard observable */
type panickingTextError struct{}

func (instance *panickingTextError) Error() string {
    panic("the serializer's error text panicked")
}

type panickingTextSerializer struct {
    cachecontract.Serializer
}

func (instance *panickingTextSerializer) Serialize(value any) ([]byte, error) {
    return nil, &panickingTextError{}
}

func TestManager_Set_NamesTheKeyOnSerializationFailure(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    setErr := manager.Set(testRuntime(), "unserializable", make(chan int), bound.UnlimitedDuration(), cachecontract.SetOptions{})
    if nil == setErr {
        t.Fatalf("expected a serialization error")
    }

    var exceptionErr *exception.Error
    if false == errors.As(setErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setErr)
    }
    if "unserializable" != exceptionErr.Context()["key"] {
        t.Fatalf("expected the error context to name the key, got: %v", exceptionErr.Context())
    }

    setMultipleErr := manager.SetMultiple(testRuntime(), map[string]any{"multi.bad": make(chan int)}, bound.UnlimitedDuration(), cachecontract.SetOptions{})
    if nil == setMultipleErr {
        t.Fatalf("expected a serialization error from SetMultiple")
    }
    if false == errors.As(setMultipleErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", setMultipleErr)
    }
    if "multi.bad" != exceptionErr.Context()["key"] {
        t.Fatalf("expected the SetMultiple error context to name the key, got: %v", exceptionErr.Context())
    }
}

type cacheTestTypedNilErrorBackend struct {
    inner *InMemoryBackend
}

func (instance *cacheTestTypedNilErrorBackend) typedNilError() error {
    var typedNil *exception.Error

    return typedNil
}

func (instance *cacheTestTypedNilErrorBackend) Get(runtimeInstance containercontract.Runtime, key string) ([]byte, bool, error) {
    payload, exists, _ := instance.inner.Get(runtimeInstance, key)
    return payload, exists, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Set(runtimeInstance containercontract.Runtime, key string, payload []byte, ttl bound.Duration, options cachecontract.SetOptions) error {
    _ = instance.inner.Set(runtimeInstance, key, payload, ttl, options)
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Delete(runtimeInstance containercontract.Runtime, key string) error {
    _ = instance.inner.Delete(runtimeInstance, key)
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Has(runtimeInstance containercontract.Runtime, key string) (bool, error) {
    hasValue, _ := instance.inner.Has(runtimeInstance, key)
    return hasValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Clear(runtimeInstance containercontract.Runtime) error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Many(runtimeInstance containercontract.Runtime, keys []string) (map[string][]byte, error) {
    result, _ := instance.inner.Many(runtimeInstance, keys)
    return result, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) SetMultiple(runtimeInstance containercontract.Runtime, items map[string][]byte, ttl bound.Duration, _ cachecontract.SetOptions) error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) DeleteMultiple(runtimeInstance containercontract.Runtime, keys []string) error {
    return instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Increment(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    newValue, _ := instance.inner.Increment(runtimeInstance, key, delta, options)
    return newValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) Decrement(runtimeInstance containercontract.Runtime, key string, delta int64, options cachecontract.CounterOptions) (int64, error) {
    newValue, _ := instance.inner.Decrement(runtimeInstance, key, delta, options)
    return newValue, instance.typedNilError()
}

func (instance *cacheTestTypedNilErrorBackend) CloseWithContext(closeContext context.Context) error {
    return instance.typedNilError()
}

func TestManager_ReadsTypedNilBackendErrorsAsSuccess(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    inner := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer inner.CloseWithContext(context.Background())

    manager := NewManager(&cacheTestTypedNilErrorBackend{inner: inner}, NewJsonSerializer(), ManagerConfig{})

    if setErr := manager.Set(testRuntime(), "a", "1", bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("expected the typed-nil Set error to read as success, got: %v", setErr)
    }

    value, exists, getErr := Get[any](For(testRuntime(), manager), "a")
    if nil != getErr {
        t.Fatalf("expected the typed-nil Get error to read as success, got: %v", getErr)
    }
    if false == exists || "1" != value.(string) {
        t.Fatalf("expected the stored value back, got exists=%v value=%v", exists, value)
    }
}

func TestManager_GetCounterReadsWhatIncrementWrote(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    if _, incrementErr := manager.Increment(testRuntime(), "visits", 5, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    value, exists, getCounterErr := manager.GetCounter(testRuntime(), "visits")
    if nil != getCounterErr {
        t.Fatalf("unexpected error: %v", getCounterErr)
    }

    if false == exists {
        t.Fatalf("expected the counter to exist")
    }

    if 5 != value {
        t.Fatalf("expected the counter to read back as an int64, got %d", value)
    }
}

func TestManager_GetCounterReportsAnAbsentKey(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    if _, exists, getCounterErr := manager.GetCounter(testRuntime(), "missing"); nil != getCounterErr || true == exists {
        t.Fatalf("expected an absent counter to report exists=false with no error, got exists=%v err=%v", exists, getCounterErr)
    }
}

func TestManager_GetCounterRejectsANonCounterPayload(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    if setErr := manager.Set(testRuntime(), "greeting", "hello", bound.Of(time.Hour), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    if _, _, getCounterErr := manager.GetCounter(testRuntime(), "greeting"); nil == getCounterErr {
        t.Fatalf("expected a serialized value to be refused by GetCounter")
    }
}

/* GetCounter refuses a payload that is not a counter with the key and the payload's length, never the payload, which strconv's own refusal quotes whole. */
func TestManager_GetCounterKeepsThePayloadOutOfTheChain(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    if setErr := manager.Set(testRuntime(), "greeting", "a-secret-greeting", bound.Of(time.Hour), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    _, _, getCounterErr := manager.GetCounter(testRuntime(), "greeting")
    if nil == getCounterErr {
        t.Fatalf("expected a serialized value to be refused by GetCounter")
    }

    if rendered := fmt.Sprintf("%v %v", getCounterErr, exception.LogContext(getCounterErr, exception.LogContextOptions{})); true == strings.Contains(rendered, "a-secret-greeting") {
        t.Fatalf("expected the payload withheld from the whole chain, got %s", rendered)
    }

    if melodyErr, isMelodyErr := getCounterErr.(*exception.Error); false == isMelodyErr || "greeting" != melodyErr.Context()["key"] || nil == melodyErr.Context()["payloadLength"] {
        t.Fatalf("expected the key and the payload length, got %v", getCounterErr)
    }
}

func TestManager_DecrementCountsDownAndLeavesACounterGetCounterCanRead(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if _, incrementErr := manager.Increment(testRuntime(), "remaining", 10, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr {
        t.Fatalf("unexpected increment error: %v", incrementErr)
    }

    newValue, decrementErr := manager.Decrement(testRuntime(), "remaining", 3, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if 7 != newValue {
        t.Fatalf("expected the decrement to count down to 7, got %d", newValue)
    }

    storedValue, exists, getCounterErr := manager.GetCounter(testRuntime(), "remaining")
    if nil != getCounterErr || false == exists {
        t.Fatalf("unexpected get counter result: %t, %v", exists, getCounterErr)
    }

    if 7 != storedValue {
        t.Fatalf("expected the stored counter to be 7, got %d", storedValue)
    }

    /* a counter written by Decrement crosses zero into the negatives rather than clamping: a remaining-quota counter that stopped at zero would report the same value for "exactly used up" and "overdrawn by a thousand" */
    newValue, decrementErr = manager.Decrement(testRuntime(), "remaining", 10, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if -3 != newValue {
        t.Fatalf("expected the counter to cross zero, got %d", newValue)
    }
}

func TestManager_DecrementOnAnAbsentKeyStartsFromZero(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    newValue, decrementErr := manager.Decrement(testRuntime(), "never.written", 2, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != decrementErr {
        t.Fatalf("unexpected decrement error: %v", decrementErr)
    }

    if -2 != newValue {
        t.Fatalf("expected an absent counter to start from zero, got %d", newValue)
    }
}

func TestManager_ABackendFailureTravelsOutOfEveryRead(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    if setErr := manager.Set(testRuntime(), "key", "value", bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    value, exists, getErr := Get[any](For(testRuntime(), manager), "key")
    if nil == getErr {
        t.Fatalf("expected Get to report the closed backend, got %v, %t", value, exists)
    }
    if nil != value || true == exists {
        t.Fatalf("expected Get to answer nothing alongside the failure, got %v, %t", value, exists)
    }
    if "cache backend is closed" != getErr.Error() {
        t.Fatalf("unexpected Get error: %v", getErr)
    }

    manyResult, manyErr := Many[any](For(testRuntime(), manager), []string{"key"})
    if nil == manyErr {
        t.Fatalf("expected Many to report the closed backend, got %v", manyResult)
    }
    if nil != manyResult {
        t.Fatalf("expected Many to answer nothing alongside the failure, got %v", manyResult)
    }
    if "cache backend is closed" != manyErr.Error() {
        t.Fatalf("unexpected Many error: %v", manyErr)
    }

    counterValue, counterExists, counterErr := manager.GetCounter(testRuntime(), "key")
    if nil == counterErr {
        t.Fatalf("expected GetCounter to report the closed backend, got %d, %t", counterValue, counterExists)
    }
    if 0 != counterValue || true == counterExists {
        t.Fatalf("expected GetCounter to answer nothing alongside the failure, got %d, %t", counterValue, counterExists)
    }
    if "cache backend is closed" != counterErr.Error() {
        t.Fatalf("unexpected GetCounter error: %v", counterErr)
    }
}

func TestManager_ManyDeserializesARepeatedKeyOnlyOnce(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}

    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    defer backend.CloseWithContext(context.Background())

    countingSerializer := &cacheTestCountingSerializer{inner: NewJsonSerializer()}
    manager := NewManager(backend, countingSerializer, ManagerConfig{})

    if setErr := manager.Set(testRuntime(), "key", "value", bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("unexpected set error: %v", setErr)
    }

    result, manyErr := Many[any](For(testRuntime(), manager), []string{"key", "key", "key"})
    if nil != manyErr {
        t.Fatalf("unexpected many error: %v", manyErr)
    }

    if 1 != len(result) {
        t.Fatalf("expected the repeated key to appear once, got %#v", result)
    }

    if "value" != result["key"] {
        t.Fatalf("unexpected value: %v", result["key"])
    }

    if 1 != countingSerializer.deserializeCallCount {
        t.Fatalf("expected exactly one deserialization for the repeated key, got %d", countingSerializer.deserializeCallCount)
    }
}

type cacheTestCountingSerializer struct {
    inner                cachecontract.Serializer
    deserializeCallCount int
}

func (instance *cacheTestCountingSerializer) Serialize(value any) ([]byte, error) {
    return instance.inner.Serialize(value)
}

func (instance *cacheTestCountingSerializer) Deserialize(payload []byte, target any) error {
    instance.deserializeCallCount = instance.deserializeCallCount + 1

    return instance.inner.Deserialize(payload, target)
}

/* typedNilBackend and typedNilSerializer embed their interfaces, so a nil pointer of either is a typed nil that still satisfies the contract the constructor is handed */
type typedNilBackend struct {
    cachecontract.Backend
}

type typedNilSerializer struct {
    cachecontract.Serializer
}

/* a typed nil passes the plain comparison, is stored as a live collaborator and dereferences its nil receiver on the first cache call — on the request path, in place of the construction-time refusal. The refusal above is pinned with an UNTYPED nil on every major, which a plain comparison answers identically, so the interface-aware half was unpinned until here. */
func TestNewManager_RefusesATypedNilBackendAndATypedNilSerializer(t *testing.T) {
    var nilBackend *typedNilBackend
    var nilSerializer *typedNilSerializer

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected the typed-nil backend to be refused at construction")
            }
        }()

        _ = NewManager(nilBackend, NewJsonSerializer(), ManagerConfig{})
    }()

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected the typed-nil serializer to be refused at construction")
            }
        }()

        clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
        backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
        defer backend.CloseWithContext(context.Background())

        _ = NewManager(backend, nilSerializer, ManagerConfig{})
    }()
}

/* the reasons are as many as the refused keys, and one of them can be the error's: a batch refused for several reasons reported one cause under a list of keys, leaving the operator to say which key it belonged to. The empty key is the one the sorted list names first whenever the batch carries it, which is a key the backend contract calls malformed — so "key" alone is not the whole answer. */
func TestManager_SetMultipleCarriesTheReasonOfEveryRefusedKey(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    setErr := manager.SetMultiple(testRuntime(), map[string]any{
        "":     make(chan int),
        "mmm":  func() {},
        "fine": "value",
    }, bound.Of(time.Minute), cachecontract.SetOptions{})

    if nil == setErr {
        t.Fatalf("expected the unserializable entries to be refused")
    }

    context := exception.LogContext(setErr, exception.LogContextOptions{})

    causeByKey, hasCauses := context["causeByKey"].(map[string]string)
    if false == hasCauses || 2 != len(causeByKey) {
        t.Fatalf("expected a reason per refused key, got %v", context["causeByKey"])
    }

    if "" == causeByKey[""] || "" == causeByKey["mmm"] {
        t.Fatalf("expected both refused keys to carry their own reason, got %v", causeByKey)
    }

    if causeByKey[""] == causeByKey["mmm"] {
        t.Fatalf("expected the two reasons to be the ones their own keys raised, got %v", causeByKey)
    }
}

func TestManager_GetCounterRefusesWhatIncrementRefuses(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: &cacheTestClock{now: time.Unix(10, 0)}})
    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    for _, payload := range []string{" 7", "+7", "07", "7\n"} {
        if setErr := backend.Set(testRuntime(), "visits", []byte(payload), bound.UnlimitedDuration(), cachecontract.SetOptions{}); nil != setErr {
            t.Fatalf("unexpected set error: %v", setErr)
        }

        if _, _, getCounterErr := manager.GetCounter(testRuntime(), "visits"); nil == getCounterErr {
            t.Fatalf("expected %q refused as no counter", payload)
        }

        if _, incrementErr := manager.Increment(testRuntime(), "visits", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
            t.Fatalf("expected Increment to refuse %q as well", payload)
        }
    }
}

func TestManager_EveryDoorHandsTheCallersContextToTheBackend(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, NewJsonSerializer(), ManagerConfig{})

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    doors := map[string]func() error{
        "Get":            func() error { _, _, err := Get[any](For(testRuntimeUnder(canceledContext), manager), "key"); return err },
        "Set":            func() error { return manager.Set(testRuntimeUnder(canceledContext), "key", "value", bound.Of(time.Minute), cachecontract.SetOptions{}) },
        "Delete":         func() error { return manager.Delete(testRuntimeUnder(canceledContext), "key") },
        "Has":            func() error { _, err := manager.Has(testRuntimeUnder(canceledContext), "key"); return err },
        "Clear":          func() error { return manager.Clear(testRuntimeUnder(canceledContext)) },
        "Many":           func() error { _, err := Many[any](For(testRuntimeUnder(canceledContext), manager), []string{"key"}); return err },
        "SetMultiple":    func() error { return manager.SetMultiple(testRuntimeUnder(canceledContext), map[string]any{"key": "value"}, bound.Of(time.Minute), cachecontract.SetOptions{}) },
        "DeleteMultiple": func() error { return manager.DeleteMultiple(testRuntimeUnder(canceledContext), []string{"key"}) },
        "Increment":      func() error { _, err := manager.Increment(testRuntimeUnder(canceledContext), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
        "Decrement":      func() error { _, err := manager.Decrement(testRuntimeUnder(canceledContext), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
        "GetCounter":     func() error { _, _, err := manager.GetCounter(testRuntimeUnder(canceledContext), "counter"); return err },
    }

    for door, call := range doors {
        if doorErr := call(); false == errors.Is(doorErr, context.Canceled) {
            t.Fatalf("%s: expected the caller's done context to reach the backend, got %v", door, doorErr)
        }
    }
}

type refusingSerializer struct{}

func (instance refusingSerializer) Serialize(value any) ([]byte, error) {
    return nil, errors.New("serialization refused")
}

func (instance refusingSerializer) Deserialize(payload []byte, target any) error {
    return errors.New("deserialization refused")
}

func TestManager_ANilRuntimeIsRefusedBeforeTheValueIsSerialized(t *testing.T) {
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour)})
    defer backend.CloseWithContext(context.Background())

    manager := NewManager(backend, refusingSerializer{}, ManagerConfig{})

    for _, refusedRuntime := range refusedTestRuntimes() {
        if setErr := manager.Set(refusedRuntime, "key", "value", bound.Of(time.Minute), cachecontract.SetOptions{}); nil == setErr || refusedRuntimeMessage != setErr.Error() {
            t.Fatalf("Set: expected the runtime refused first, got %v", setErr)
        }

        if setErr := manager.SetMultiple(refusedRuntime, map[string]any{"key": "value"}, bound.Of(time.Minute), cachecontract.SetOptions{}); nil == setErr || refusedRuntimeMessage != setErr.Error() {
            t.Fatalf("SetMultiple: expected the runtime refused first, got %v", setErr)
        }
    }
}

func TestManager_EveryDoorRefusesAnAbsentRuntime(t *testing.T) {
    manager, _ := newCacheTestManager(t)

    for _, refusedRuntime := range refusedTestRuntimes() {
        var target any
        var targets map[string]any
        _, getErr := manager.Get(refusedRuntime, "k", &target)
        setErr := manager.Set(refusedRuntime, "k", "v", bound.Of(time.Minute), cachecontract.SetOptions{})
        deleteErr := manager.Delete(refusedRuntime, "k")
        _, hasErr := manager.Has(refusedRuntime, "k")
        clearErr := manager.Clear(refusedRuntime)
        manyErr := manager.Many(refusedRuntime, []string{"k"}, &targets)
        setMultipleErr := manager.SetMultiple(refusedRuntime, map[string]any{"k": "v"}, bound.Of(time.Minute), cachecontract.SetOptions{})
        deleteMultipleErr := manager.DeleteMultiple(refusedRuntime, []string{"k"})
        _, incrementErr := manager.Increment(refusedRuntime, "c", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
        _, decrementErr := manager.Decrement(refusedRuntime, "c", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
        _, _, getCounterErr := manager.GetCounter(refusedRuntime, "c")

        for doorName, doorErr := range map[string]error{"Get": getErr, "Set": setErr, "Delete": deleteErr, "Has": hasErr, "Clear": clearErr, "Many": manyErr, "SetMultiple": setMultipleErr, "DeleteMultiple": deleteMultipleErr, "Increment": incrementErr, "Decrement": decrementErr, "GetCounter": getCounterErr} {
            if nil == doorErr || refusedRuntimeMessage != doorErr.Error() {
                t.Fatalf("%s: expected the runtime refused, got %v", doorName, doorErr)
            }
        }
    }
}

func TestManager_ACounterHandsItsOptionsToTheBackend(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    window := cachecontract.CounterOptions{Ttl: bound.Of(time.Minute)}
    manager.Increment(testRuntime(), "up", 1, window)
    manager.Decrement(testRuntime(), "down", 1, window)

    clockInstance.now = clockInstance.now.Add(61 * time.Second)
    for _, key := range []string{"up", "down"} {
        if _, exists, _ := manager.GetCounter(testRuntime(), key); true == exists {
            t.Fatalf("expected %s gone at the ttl the options carried", key)
        }
    }

    if _, refusedErr := manager.Increment(testRuntime(), "refused", 1, cachecontract.CounterOptions{}); nil == refusedErr {
        t.Fatal("expected an absent ttl refused through the manager")
    }
}

func TestManager_ASlidingCounterKeepsItsRefreshThroughTheManager(t *testing.T) {
    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    manager := NewManager(NewInMemoryBackend(InMemoryBackendConfig{CleanupInterval: bound.Of(time.Hour), Clock: clockInstance}), NewJsonSerializer(), ManagerConfig{OwnsBackend: true})
    defer manager.CloseWithContext(context.Background())

    sliding := cachecontract.CounterOptions{Ttl: bound.Of(time.Minute), RefreshTtl: true}
    manager.Increment(testRuntime(), "hits", 1, sliding)

    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    manager.Decrement(testRuntime(), "hits", 1, sliding)

    clockInstance.now = clockInstance.now.Add(40 * time.Second)
    if _, exists, _ := manager.GetCounter(testRuntime(), "hits"); false == exists {
        t.Fatal("expected the sliding window kept alive through the manager")
    }
}
